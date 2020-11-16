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
	"strings"

	"github.com/yl2chen/cidranger"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/dns"
	"istio.io/istio/pkg/config/host"
	"istio.io/pkg/log"
)

type gateways struct {
	dnsNames     dns.NameSet
	serviceNames dns.NameSet
}

func newGateways() gateways {
	return gateways{
		dnsNames:     dns.NewNameSet(),
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

func (c *Controller) canonicalServiceName(name string) host.Name {
	segments := strings.SplitN(name, ".", 3)
	switch len(segments) {
	case 1:
		return kube.ServiceHostname(segments[0], IstioNamespace, c.domainSuffix)
	default:
		return kube.ServiceHostname(segments[0], segments[1], c.domainSuffix)
	}
}

// reloadNetworkLookup will read the mesh networks configuration from the environment
// and initialize CIDR rangers for an efficient network lookup when needed
func (c *Controller) reloadNetworkLookup() {
	c.Lock()

	c.networkForRegistry = ""
	oldGateways := c.gateways
	c.gateways = newGateways()

	ranger := cidranger.NewPCTrieRanger()

	meshNetworks := c.networksWatcher.Networks()
	for n, v := range meshNetworks.GetNetworks() {
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
				c.networkForRegistry = n
			}
		}

		for _, gw := range v.Gateways {
			if gwAddress := gw.GetAddress(); gwAddress != "" && net.ParseIP(gwAddress) == nil {
				c.gateways.dnsNames.Add(gwAddress)
			}

			// track which services from this registry act as gateways for what networks
			if c.networkForRegistry == n {
				if gwSvcName := gw.GetRegistryServiceName(); gwSvcName != "" {
					c.gateways.serviceNames.Add(string(c.canonicalServiceName(gwSvcName)))
				}
			}
		}
	}
	c.ranger = ranger
	c.configureDNSResolver(oldGateways, c.gateways)
	c.Unlock()
	// the network for endpoints are computed when we process the events; this will fix the cache
	// NOTE: this must run before the other network watcher handler that creates a force push
	if err := c.syncPods(); err != nil {
		log.Errorf("one or more errors force-syncing pods: %v", err)
	}
	if err := c.syncEndpoints(); err != nil {
		log.Errorf("one or more errors force-syncing endpoints: %v", err)
	}
}

func (c *Controller) configureDNSResolver(prev, next gateways) {
	log.Debugf("Re-configuring DNS resolver on mesh networks change: old gateways=%#v, new gateways=%#v", prev, next)

	log.Debugf("Start watching for DNS names from the MeshNetworks config: %v", next.dnsNames.List())
	if c.dnsResolver != nil {
		c.dnsResolver.Watch(dns.Referer{APIGroup: "istio.mesh", Kind: "MeshNetworks"}, next.dnsNames.List())
	}

	for serviceName := range next.serviceNames {
		c.watchServiceDNSNames(serviceName)
	}

	_, deleted := next.serviceNames.Diff(prev.serviceNames)
	for serviceName := range deleted {
		c.forgetServiceDNSNames(serviceName)
	}
}

func (c *Controller) watchServiceDNSNames(serviceName string) {
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
		c.dnsResolver.Watch(dns.Referer{Kind: "Service", Name: serviceName}, dnsNames.List())
	}
}

func (c *Controller) forgetServiceDNSNames(serviceName string) {
	log.Debugf("Stop watching for DNS names of a gateway Service %q", serviceName)
	if c.dnsResolver != nil {
		c.dnsResolver.Cancel(dns.Referer{Kind: "Service", Name: serviceName})
	}
}

func (c *Controller) watchGatewayServiceDNSNames(serviceName host.Name) {
	name := string(serviceName)
	if c.gateways.serviceNames.Contains(name) {
		c.watchServiceDNSNames(name)
	}
}

func (c *Controller) forgetGatewayServiceDNSNames(serviceName host.Name) {
	name := string(serviceName)
	if c.gateways.serviceNames.Contains(name) {
		c.forgetServiceDNSNames(name)
	}
}

// return the mesh network for the endpoint IP. Empty string if not found.
func (c *Controller) endpointNetwork(endpointIP string) string {
	// If networkForRegistry is set then all endpoints discovered by this registry
	// belong to the configured network so simply return it
	if len(c.networkForRegistry) != 0 {
		return c.networkForRegistry
	}

	// Try to determine the network by checking whether the endpoint IP belongs
	// to any of the configure networks' CIDR ranges
	if c.ranger == nil {
		return ""
	}
	entries, err := c.ranger.ContainingNetworks(net.ParseIP(endpointIP))
	if err != nil {
		log.Errora(err)
		return ""
	}
	if len(entries) == 0 {
		return ""
	}
	if len(entries) > 1 {
		log.Warnf("Found multiple networks CIDRs matching the endpoint IP: %s. Using the first match.", endpointIP)
	}

	return (entries[0].(namedRangerEntry)).name
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
