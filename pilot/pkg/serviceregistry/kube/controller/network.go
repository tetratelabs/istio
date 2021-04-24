// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"net"
	"strconv"
	"strings"

	"github.com/yl2chen/cidranger"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/dns"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

// fixedGateways represents an index of network gateways according to Istio MeshNetworks configuration.
type fixedGateways struct {
	dnsNames     dns.NameSet
	serviceNames dns.NameSet
}

func newFixedGateways() fixedGateways {
	return fixedGateways{
		dnsNames:     dns.NewNameSet(),
		serviceNames: dns.NewNameSet(),
	}
}

// dynamicGateways represents an index of network gateways according to labels on k8s Services.
type dynamicGateways struct {
	serviceNames dns.NameSet
}

func newDynamicGateways() dynamicGateways {
	return dynamicGateways{
		serviceNames: dns.NewNameSet(),
	}
}

// namedRangerEntry for holding network's CIDR and name
type namedRangerEntry struct {
	name    string
	network net.IPNet
}

// returns the IPNet for the network
func (n namedRangerEntry) Network() net.IPNet {
	return n.network
}

func (c *Controller) onNetworkChanged() {
	// the network for endpoints are computed when we process the events; this will fix the cache
	// NOTE: this must run before the other network watcher handler that creates a force push
	if err := c.syncPods(); err != nil {
		log.Errorf("one or more errors force-syncing pods: %v", err)
	}
	if err := c.syncEndpoints(); err != nil {
		log.Errorf("one or more errors force-syncing endpoints: %v", err)
	}
	c.reloadNetworkGateways()
}

// reloadNetworkLookup refreshes the meshNetworks configuration, network for each endpoint, and
// recomputes network gateways.
func (c *Controller) reloadNetworkLookup() {
	c.reloadMeshNetworks()
	c.onNetworkChanged()
}

func (c *Controller) canonicalServiceName(name string) host.Name {
	segments := strings.SplitN(name, ".", 3)
	switch len(segments) {
	case 1:
		return kube.ServiceHostname(segments[0], IstioNamespace, c.domainSuffix)
	default:
		return kube.ServiceHostname(segments[0], segments[1], c.domainSuffix)
	}
}

// reloadMeshNetworks will read the mesh networks configuration to setup
// fromRegistry and cidr based network lookups for this registry
func (c *Controller) reloadMeshNetworks() {
	c.Lock()
	defer c.Unlock()
	c.networkForRegistry = ""

	ranger := cidranger.NewPCTrieRanger()

	c.networkForRegistry = ""
	c.registryServiceNameGateways = map[host.Name]uint32{}
	oldFixedGateways := c.fixedGateways
	c.fixedGateways = newFixedGateways()

	meshNetworks := c.networksWatcher.Networks()
	for n, v := range meshNetworks.GetNetworks() {
		// track endpoints items from this registry are a part of this network
		for _, ep := range v.Endpoints {
			if ep.GetFromCidr() != "" {
				_, network, err := net.ParseCIDR(ep.GetFromCidr())
				if err != nil {
					log.Warnf("unable to parse CIDR %q for network %s", ep.GetFromCidr(), n)
					continue
				}
				rangerEntry := namedRangerEntry{
					name:    n,
					network: *network,
				}
				_ = ranger.Insert(rangerEntry)
			}
			if ep.GetFromRegistry() != "" && ep.GetFromRegistry() == c.clusterID {
				if c.networkForRegistry != "" {
					log.Warnf("multiple networks specify %s in fromRegistry, only first network %s will use %s",
						c.clusterID, c.networkForRegistry, c.clusterID)
				} else {
					c.networkForRegistry = n
				}
			}
		}

		for _, gw := range v.Gateways {
			if gwAddress := gw.GetAddress(); gwAddress != "" && net.ParseIP(gwAddress) == nil {
				c.fixedGateways.dnsNames.Add(gwAddress)
			}

			// track which services from this registry act as gateways for what networks
			if c.networkForRegistry == n {
				if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
					c.registryServiceNameGateways[host.Name(gwSvcName)] = gw.Port
					c.fixedGateways.serviceNames.Add(string(c.canonicalServiceName(gwSvcName)))
				}
			}
		}
	}
	c.configureDNSResolver(oldFixedGateways, c.fixedGateways)
	c.ranger = ranger
}

func (c *Controller) configureDNSResolver(prev, next fixedGateways) {
	log.Debugf("Re-configuring DNS resolver on mesh networks change: old gateways=%#v, new gateways=%#v", prev, next)

	log.Debugf("Start watching for DNS names from the MeshNetworks config: %v", next.dnsNames.List())
	if c.dnsResolver != nil {
		c.dnsResolver.Watch(dns.Referer{APIGroup: "istio.mesh", Kind: "MeshNetworks"}, next.dnsNames.List())
	}

	for serviceName := range next.serviceNames {
		c.watchFixedGatewayServiceDNSNames(host.Name(serviceName))
	}

	_, deleted := next.serviceNames.Diff(prev.serviceNames)
	for serviceName := range deleted {
		c.forgetFixedGatewayServiceDNSNames(host.Name(serviceName))
	}
}

func (c *Controller) watchServiceDNSNames(serviceName, source string) {
	dnsNames := dns.NewNameSet()

	svc := c.servicesMap[host.Name(serviceName)]
	if svc != nil && svc.Attributes.ClusterExternalAddresses != nil {
		addresses := svc.Attributes.ClusterExternalAddresses[c.clusterID]
		for _, address := range addresses {
			if net.ParseIP(address) == nil {
				dnsNames.Add(address)
			}
		}
	}

	log.Debugf("Start watching for DNS names of a gateway Service %q: %v", serviceName, dnsNames.List())
	if c.dnsResolver != nil {
		c.dnsResolver.Watch(dns.Referer{Source: source, Kind: "Service", Name: serviceName}, dnsNames.List())
	}
}

func (c *Controller) forgetServiceDNSNames(serviceName, source string) {
	log.Debugf("Stop watching for DNS names of a gateway Service %q", serviceName)
	if c.dnsResolver != nil {
		c.dnsResolver.Cancel(dns.Referer{Source: source, Kind: "Service", Name: serviceName})
	}
}

func (c *Controller) watchFixedGatewayServiceDNSNames(serviceName host.Name) {
	name := string(serviceName)
	if c.fixedGateways.serviceNames.Contains(name) {
		c.watchServiceDNSNames(name, "MeshNetworks")
	}
}

func (c *Controller) forgetFixedGatewayServiceDNSNames(serviceName host.Name) {
	name := string(serviceName)
	if c.fixedGateways.serviceNames.Contains(name) {
		c.forgetServiceDNSNames(name, "MeshNetworks")
	}
}

func (c *Controller) watchDynamicGatewayServiceDNSNames(serviceName host.Name) {
	name := string(serviceName)
	c.dynamicGateways.serviceNames.Add(name)
	c.watchServiceDNSNames(name, "k8s")
}

func (c *Controller) forgetDynamicGatewayServiceDNSNames(serviceName host.Name) {
	name := string(serviceName)
	if c.dynamicGateways.serviceNames.Remove(name) {
		c.forgetServiceDNSNames(name, "k8s")
	}
}

func (c *Controller) NetworkGateways() map[string][]*model.Gateway {
	c.RLock()
	defer c.RUnlock()
	if c.networkGateways == nil || len(c.networkGateways) == 0 {
		return nil
	}
	gws := map[string][]*model.Gateway{}
	for _, netGws := range c.networkGateways {
		if netGws == nil {
			continue
		}
		for nw, gw := range netGws {
			gws[nw] = append(gws[nw], gw...)
		}
	}
	return gws
}

// extractGatewaysFromService checks if the service is a cross-network gateway
// and if it is, updates the controller's gateways.
func (c *Controller) extractGatewaysFromService(svc *model.Service) {
	c.Lock()
	defer c.Unlock()
	c.extractGatewaysInner(svc)
}

// reloadNetworkGateways performs extractGatewaysFromService for all services registered with the controller.
func (c *Controller) reloadNetworkGateways() {
	c.Lock()
	defer c.Unlock()
	for _, svc := range c.servicesMap {
		c.extractGatewaysInner(svc)
	}
}

// extractGatewaysInner performs the logic for extractGatewaysFromService without locking the controller
func (c *Controller) extractGatewaysInner(svc *model.Service) {
	svc.Mutex.RLock()
	defer svc.Mutex.RUnlock()

	gwPort, network := c.getGatewayDetails(svc)
	if gwPort == 0 {
		// not a gateway
		return
	}

	if c.networkGateways[svc.Hostname] == nil {
		c.networkGateways[svc.Hostname] = map[string][]*model.Gateway{}
	}

	gws := make([]*model.Gateway, 0, len(svc.Attributes.ClusterExternalAddresses))

	// TODO(landow) ClusterExternalAddresses doesn't need to get used outside of the kube controller, and spreads
	// TODO(cont)   logic between ConvertService, extractGatewaysInner, and updateServiceNodePortAddresses.
	if svc.Attributes.ClusterExternalAddresses != nil {
		// check if we have node port mappings
		if svc.Attributes.ClusterExternalPorts != nil {
			if nodePortMap, exists := svc.Attributes.ClusterExternalPorts[c.clusterID]; exists {
				// what we now have is a service port. If there is a mapping for cluster external ports,
				// look it up and get the node port for the remote port
				if nodePort, exists := nodePortMap[gwPort]; exists {
					gwPort = nodePort
				}
			}
		}
		ips := svc.Attributes.ClusterExternalAddresses[c.clusterID]
		for _, ip := range ips {
			gws = append(gws, &model.Gateway{Addr: ip, Port: gwPort})
		}
	}
	c.networkGateways[svc.Hostname][network] = gws
}

func (c *Controller) isDynamicGatewayService(svc *model.Service) bool {
	_, present := svc.Attributes.Labels[label.IstioNetwork]
	return present
}

// getGatewayDetails finds the port and network to use for cross-network traffic on the given service.
// Zero values are returned if the service is not a cross-network gateway.
func (c *Controller) getGatewayDetails(svc *model.Service) (uint32, string) {
	// label based gateways
	if nw, present := svc.Attributes.Labels[label.IstioNetwork]; present {
		if gwPortStr := svc.Attributes.Labels[IstioGatewayPortLabel]; gwPortStr != "" {
			if gwPort, err := strconv.Atoi(gwPortStr); err == nil {
				return uint32(gwPort), nw
			}
			log.Warnf("could not parse %q for %s on %s/%s; defaulting to %d",
				gwPortStr, IstioGatewayPortLabel, svc.Attributes.Namespace, svc.Attributes.Name, DefaultNetworkGatewayPort)
		}
		return DefaultNetworkGatewayPort, nw
	}

	// meshNetworks registryServiceName+fromRegistry
	if port, ok := c.registryServiceNameGateways[svc.Hostname]; ok {
		return port, c.networkForRegistry
	}

	return 0, ""
}

// updateServiceNodePortAddresses updates ClusterExternalAddresses for Services of nodePort type
func (c *Controller) updateServiceNodePortAddresses(svcs ...*model.Service) bool {
	// node event, update all nodePort gateway services
	if len(svcs) == 0 {
		svcs = c.getNodePortGatewayServices()
	}
	// no nodePort gateway service found, no update
	if len(svcs) == 0 {
		return false
	}
	for _, svc := range svcs {
		c.RLock()
		nodeSelector := c.nodeSelectorsForServices[svc.Hostname]
		c.RUnlock()
		// update external address
		svc.Mutex.Lock()
		if nodeSelector == nil {
			var extAddresses []string
			for _, n := range c.nodeInfoMap {
				extAddresses = append(extAddresses, n.address)
			}
			svc.Attributes.ClusterExternalAddresses = map[string][]string{c.clusterID: extAddresses}
		} else {
			var nodeAddresses []string
			for _, n := range c.nodeInfoMap {
				if nodeSelector.SubsetOf(n.labels) {
					nodeAddresses = append(nodeAddresses, n.address)
				}
			}
			svc.Attributes.ClusterExternalAddresses = map[string][]string{c.clusterID: nodeAddresses}
		}
		svc.Mutex.Unlock()
		// update gateways that use the service
		c.extractGatewaysFromService(svc)
	}
	return true
}

// getNodePortServices returns nodePort type gateway service
func (c *Controller) getNodePortGatewayServices() []*model.Service {
	c.RLock()
	defer c.RUnlock()
	out := make([]*model.Service, 0, len(c.nodeSelectorsForServices))
	for hostname := range c.nodeSelectorsForServices {
		svc := c.servicesMap[hostname]
		if svc != nil {
			out = append(out, svc)
		}
	}

	return out
}

func (c *Controller) isGatewayDNS(dnsName string) bool {
	return true // TODO(yskopets): optimize once DNSResolver is used for anything other than gateways
}

func (c *Controller) refreshGatewayEndpoints(dnsName string) {
	if c.isGatewayDNS(dnsName) {
		log.Debugf("Triggering a full xDS push since DNS name %q of a network gateway is now resolved into a different set of IP addresses", dnsName)
		c.xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full: true,
		})
	}
}
