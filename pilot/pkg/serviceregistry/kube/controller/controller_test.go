// Copyright 2017 Istio Authors
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
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	coreV1 "k8s.io/api/core/v1"
	discoveryv1alpha1 "k8s.io/api/discovery/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	metafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	testService  = "test"
	resync       = 1 * time.Second
	domainSuffix = "company.com"
)

func (fx *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
	select {
	case fx.Events <- XdsEvent{Type: "xds"}:
	default:
	}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
	select {
	case fx.Events <- XdsEvent{Type: "proxy"}:
	default:
	}
}

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan XdsEvent
}

// XdsEvent is used to watch XdsEvents
type XdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string

	// The endpoints associated with an EDS push if any
	Endpoints []*model.IstioEndpoint
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan XdsEvent, 100),
	}
}

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, _ string, entry []*model.IstioEndpoint) error {
	if len(entry) > 0 {
		select {
		case fx.Events <- XdsEvent{Type: "eds", ID: hostname, Endpoints: entry}:
		default:
		}

	}
	return nil
}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(_, hostname string, _ string, _ model.Event) {
	select {
	case fx.Events <- XdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) Wait(et string) *XdsEvent {
	for {
		select {
		case e := <-fx.Events:
			if e.Type == et {
				return &e
			}
			continue
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

// Clear any pending event
func (fx *FakeXdsUpdater) Clear() {
	wait := true
	for wait {
		select {
		case <-fx.Events:
		default:
			wait = false
		}
	}
}

type fakeControllerOptions struct {
	networksWatcher mesh.NetworksWatcher
	serviceHandler  func(service *model.Service, event model.Event)
	instanceHandler func(instance *model.ServiceInstance, event model.Event)
	mode            EndpointMode
	clusterID       string
}

func newFakeControllerWithOptions(opts fakeControllerOptions) (*Controller, *FakeXdsUpdater) {
	fx := NewFakeXDS()

	clientSet := fake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	metaV1.AddMetaToScheme(scheme)
	metadataClient := metafake.NewSimpleMetadataClient(scheme)

	c := NewController(clientSet, metadataClient, Options{
		WatchedNamespace: "", // tests create resources in multiple ns
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
		XDSUpdater:       fx,
		Metrics:          &model.Environment{},
		NetworksWatcher:  opts.networksWatcher,
		EndpointMode:     opts.mode,
		ClusterID:        opts.clusterID,
	})

	if opts.instanceHandler != nil {
		_ = c.AppendInstanceHandler(opts.instanceHandler)
	}
	if opts.serviceHandler != nil {
		_ = c.AppendServiceHandler(opts.serviceHandler)
	}
	c.stop = make(chan struct{})
	// Run in initiation to prevent calling each test
	// TODO: fix it, so we can remove `stop` channel
	go c.Run(c.stop)
	// Wait for the caches to sync, otherwise we may hit race conditions where events are dropped
	cache.WaitForCacheSync(c.stop, c.nodeMetadataInformer.HasSynced, c.pods.informer.HasSynced,
		c.services.HasSynced)
	return c, fx
}

func TestServices(t *testing.T) {

	networksWatcher := mesh.NewFixedNetworksWatcher(&meshconfig.MeshNetworks{
		Networks: map[string]*meshconfig.Network{
			"network1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{
					{
						Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
							FromCidr: "10.10.1.1/24",
						},
					},
				},
			},
			"network2": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{
					{
						Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
							FromCidr: "10.11.1.1/24",
						},
					},
				},
			},
		},
	})

	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			ctl, fx := newFakeControllerWithOptions(fakeControllerOptions{networksWatcher: networksWatcher, mode: mode})
			defer ctl.Stop()
			t.Parallel()
			ns := "ns-test"

			hostname := kube.ServiceHostname(testService, ns, domainSuffix)

			var sds model.ServiceDiscovery = ctl
			// "test", ports: http-example on 80
			makeService(testService, ns, ctl.client, t)
			<-fx.Events

			test.Eventually(t, "successfully added a service", func() bool {
				out, clientErr := sds.Services()
				if clientErr != nil {
					return false
				}
				log.Infof("Services: %#v", out)

				// Original test was checking for 'protocolTCP' - which is incorrect (the
				// port name is 'http'. It was working because the Service was created with
				// an invalid protocol, and the code was ignoring that ( not TCP/UDP).
				for _, item := range out {
					if item.Hostname == hostname &&
						len(item.Ports) == 1 &&
						item.Ports[0].Protocol == protocol.HTTP {
						return true
					}
				}
				return false
			})

			// 2 ports 1001, 2 IPs
			createEndpoints(ctl, testService, ns, []string{"http-example", "foo"}, []string{"10.10.1.1", "10.11.1.2"}, nil, t)

			svc, err := sds.GetService(hostname)
			if err != nil {
				t.Fatalf("GetService(%q) encountered unexpected error: %v", hostname, err)
			}
			if svc == nil {
				t.Fatalf("GetService(%q) => should exists", hostname)
			}
			if svc.Hostname != hostname {
				t.Fatalf("GetService(%q) => %q", hostname, svc.Hostname)
			}

			test.Eventually(t, "successfully created endpoints", func() bool {
				ep, anotherErr := sds.InstancesByPort(svc, 80, nil)
				if anotherErr != nil {
					t.Fatalf("error gettings instance by port: %v", anotherErr)
					return false
				}
				if len(ep) == 2 {
					return true
				}
				return false
			})

			ep, err := sds.InstancesByPort(svc, 80, nil)
			if err != nil {
				t.Fatalf("GetInstancesByPort() encountered unexpected error: %v", err)
			}
			if len(ep) != 2 {
				t.Fatalf("Invalid response for GetInstancesByPort %v", ep)
			}

			if ep[0].Endpoint.Address == "10.10.1.1" && ep[0].Endpoint.Network != "network1" {
				t.Fatalf("Endpoint with IP 10.10.1.1 is expected to be in network1 but get: %s", ep[0].Endpoint.Network)
			}

			if ep[1].Endpoint.Address == "10.11.1.2" && ep[1].Endpoint.Network != "network2" {
				t.Fatalf("Endpoint with IP 10.11.1.2 is expected to be in network2 but get: %s", ep[1].Endpoint.Network)
			}

			missing := kube.ServiceHostname("does-not-exist", ns, domainSuffix)
			svc, err = sds.GetService(missing)
			if err != nil {
				t.Fatalf("GetService(%q) encountered unexpected error: %v", missing, err)
			}
			if svc != nil {
				t.Fatalf("GetService(%q) => %s, should not exist", missing, svc.Hostname)
			}
		})
	}
}

func makeService(n, ns string, cl kubernetes.Interface, t *testing.T) {
	_, err := cl.CoreV1().Services(ns).Create(context.TODO(), &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{Name: n},
		Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{
				{
					Port:     80,
					Name:     "http-example",
					Protocol: coreV1.ProtocolTCP, // Not added automatically by fake
				},
			},
		},
	}, metaV1.CreateOptions{})
	if err != nil {
		t.Log("Service already created (rerunning test)")
	}
	log.Infof("Created service %s", n)
}

func TestController_GetPodLocality(t *testing.T) {
	pod1 := generatePod("128.0.1.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod("128.0.1.2", "pod2", "nsB", "", "node2", map[string]string{"app": "prod-app"}, map[string]string{})
	podOverride := generatePod("128.0.1.2", "pod2", "nsB", "",
		"node1", map[string]string{"app": "prod-app", model.LocalityLabel: "regionOverride.zoneOverride.subzoneOverride"}, map[string]string{})
	testCases := []struct {
		name   string
		pods   []*coreV1.Pod
		nodes  []*coreV1.Node
		wantAZ map[*coreV1.Pod]string
	}{
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", IstioSubzoneLabel: "subzone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2", IstioSubzoneLabel: "subzone2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1/zone1/subzone1",
				pod2: "region2/zone2/subzone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1/zone1/",
				pod2: "region2/zone2/",
			},
		},
		{
			name: "should return false if pod isn't in the cache",
			wantAZ: map[*coreV1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false if node isn't in the cache",
			pods: []*coreV1.Pod{pod1, pod2},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return correct az if node has only region label",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1//",
				pod2: "region2//",
			},
		},
		{
			name: "should return correct az if node has only zone label",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "/zone1/",
				pod2: "/zone2/",
			},
		},
		{
			name: "should return correct az if node has only subzone label",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{IstioSubzoneLabel: "subzone1"}),
				generateNode("node2", map[string]string{IstioSubzoneLabel: "subzone2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "//subzone1",
				pod2: "//subzone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{podOverride},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", IstioSubzoneLabel: "subzone1"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				podOverride: "regionOverride/zoneOverride/subzoneOverride",
			},
		},
	}

	for _, tc := range testCases {
		// If using t.Parallel() you must copy the iteration to a new local variable
		// https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Setup kube caches
			// Pod locality only matters for Endpoints
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: EndpointsOnly})
			defer controller.Stop()
			addNodes(t, controller, tc.nodes...)
			addPods(t, controller, tc.pods...)
			for _, pod := range tc.pods {
				if err := waitForPod(controller, pod.Status.PodIP); err != nil {
					t.Fatalf("wait for pod err: %v", err)
				}
				// pod first time occur will trigger proxy push
				fx.Wait("proxy")
			}

			// Verify expected existing pod AZs
			for pod, wantAZ := range tc.wantAZ {
				az := controller.getPodLocality(pod)
				if wantAZ != "" {
					if !reflect.DeepEqual(az, wantAZ) {
						t.Fatalf("Wanted az: %s, got: %s", wantAZ, az)
					}
				} else {
					if az != "" {
						t.Fatalf("Unexpectedly found az: %s for pod: %s", az, pod.ObjectMeta.Name)
					}
				}
			}
		})
	}

}

func TestGetProxyServiceInstances(t *testing.T) {
	clusterID := "fakeCluster"
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{
				mode:      mode,
				clusterID: clusterID,
			})
			defer controller.Stop()
			p := generatePod("128.0.0.1", "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
			addPods(t, controller, p)
			if err := waitForPod(controller, p.Status.PodIP); err != nil {
				t.Fatalf("wait for pod err: %v", err)
			}

			k8sSaOnVM := "acct4"
			canonicalSaOnVM := "acctvm2@gserviceaccount2.com"

			createService(controller, "svc1", "nsa",
				map[string]string{
					annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
					annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM},
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)
			ev := fx.Wait("service")
			if ev == nil {
				t.Fatal("Timeout creating service")
			}

			// Endpoints are generated by Kubernetes from pod labels and service selectors.
			// Here we manually create them for mocking purpose.
			svc1Ips := []string{"128.0.0.1"}
			portNames := []string{"tcp-port"}
			// Create 1 endpoint that refers to a pod in the same namespace.
			createEndpoints(controller, "svc1", "nsA", portNames, svc1Ips, nil, t)

			// Creates 100 endpoints that refers to a pod in a different namespace.
			fakeSvcCounts := 100
			for i := 0; i < fakeSvcCounts; i++ {
				svcName := fmt.Sprintf("svc-fake-%d", i)
				createService(controller, svcName, "nsfake",
					map[string]string{
						annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
						annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM},
					[]int32{8080}, map[string]string{"app": "prod-app"}, t)
				fx.Wait("service")

				createEndpoints(controller, svcName, "nsfake", portNames, svc1Ips, nil, t)
				fx.Wait("eds")
			}

			// Create 1 endpoint that refers to a pod in the same namespace.
			createEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, nil, t)
			fx.Wait("eds")

			var svcNode model.Proxy
			svcNode.Type = model.Router
			svcNode.IPAddresses = []string{"128.0.0.1"}
			svcNode.ID = "pod1.nsa"
			svcNode.DNSDomain = "nsa.svc.cluster.local"
			svcNode.Metadata = &model.NodeMetadata{Namespace: "nsa"}
			serviceInstances, err := controller.GetProxyServiceInstances(&svcNode)
			if err != nil {
				t.Fatalf("client encountered error during GetProxyServiceInstances(): %v", err)
			}

			if len(serviceInstances) != 1 {
				t.Fatalf("GetProxyServiceInstances() expected 1 instance, got %d", len(serviceInstances))
			}

			hostname := kube.ServiceHostname("svc1", "nsa", domainSuffix)
			if serviceInstances[0].Service.Hostname != hostname {
				t.Fatalf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
					serviceInstances[0].Service.Hostname, hostname)
			}

			// Test that we can look up instances just by Proxy metadata
			metaServices, err := controller.GetProxyServiceInstances(&model.Proxy{
				Type:            "sidecar",
				IPAddresses:     []string{"1.1.1.1"},
				Locality:        &core.Locality{Region: "r", Zone: "z"},
				ConfigNamespace: "nsa",
				Metadata: &model.NodeMetadata{ServiceAccount: "account",
					ClusterID: clusterID,
					Labels: map[string]string{
						"app": "prod-app",
					}},
			})
			if err != nil {
				t.Fatalf("got err getting service instances")
			}

			expected := &model.ServiceInstance{

				Service: &model.Service{
					Hostname:        "svc1.nsa.svc.company.com",
					Address:         "10.0.0.1",
					Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
					ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
					Attributes: model.ServiceAttributes{
						ServiceRegistry: string(serviceregistry.Kubernetes),
						Name:            "svc1",
						Namespace:       "nsa",
						UID:             "istio://nsa/services/svc1",
						LabelSelectors:  map[string]string{"app": "prod-app"},
					},
				},
				ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
				Endpoint: &model.IstioEndpoint{Labels: labels.Instance{"app": "prod-app"},
					ServiceAccount:  "account",
					Address:         "1.1.1.1",
					EndpointPort:    0,
					ServicePortName: "tcp-port",
					Locality: model.Locality{
						Label:     "r/z",
						ClusterID: clusterID,
					},
				},
			}
			if len(metaServices) != 1 {
				t.Fatalf("expected 1 instance, got %v", len(metaServices))
			}
			if !reflect.DeepEqual(expected, metaServices[0]) {
				t.Fatalf("expected instance %v, got %v", expected, metaServices[0])
			}

			// Test that we first look up instances by Proxy pod

			node := generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", IstioSubzoneLabel: "subzone1"})
			addNodes(t, controller, node)

			// 1. pod without `istio-locality` label, get locality from node label.
			p = generatePod("129.0.0.1", "pod2", "nsa", "svcaccount", "node1",
				map[string]string{"app": "prod-app"}, nil)
			addPods(t, controller, p)
			if err := waitForPod(controller, p.Status.PodIP); err != nil {
				t.Fatalf("wait for pod err: %v", err)
			}

			podServices, err := controller.GetProxyServiceInstances(&model.Proxy{
				Type:            "sidecar",
				IPAddresses:     []string{"129.0.0.1"},
				Locality:        &core.Locality{Region: "r", Zone: "z"},
				ConfigNamespace: "nsa",
				Metadata: &model.NodeMetadata{ServiceAccount: "account",
					ClusterID: clusterID,
					Labels: map[string]string{
						"app": "prod-app",
					}},
			})
			if err != nil {
				t.Fatalf("got err getting service instances")
			}

			expected = &model.ServiceInstance{

				Service: &model.Service{
					Hostname:        "svc1.nsa.svc.company.com",
					Address:         "10.0.0.1",
					Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
					ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
					Attributes: model.ServiceAttributes{
						ServiceRegistry: string(serviceregistry.Kubernetes),
						Name:            "svc1",
						Namespace:       "nsa",
						UID:             "istio://nsa/services/svc1",
						LabelSelectors:  map[string]string{"app": "prod-app"},
					},
				},
				ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
				Endpoint: &model.IstioEndpoint{
					Address:         "129.0.0.1",
					EndpointPort:    0,
					ServicePortName: "tcp-port",
					Locality: model.Locality{
						Label:     "region1/zone1/subzone1",
						ClusterID: clusterID,
					},
					Labels:         labels.Instance{"app": "prod-app"},
					ServiceAccount: "spiffe://cluster.local/ns/nsa/sa/svcaccount",
					TLSMode:        model.DisabledTLSModeLabel, UID: "kubernetes://pod2.nsa",
				},
			}
			if len(podServices) != 1 {
				t.Fatalf("expected 1 instance, got %v", len(podServices))
			}
			if !reflect.DeepEqual(expected, podServices[0]) {
				t.Fatalf("expected instance %v, got %v", expected, podServices[0])
			}

			// 2. pod with `istio-locality` label, ignore node label.
			p = generatePod("129.0.0.2", "pod3", "nsa", "svcaccount", "node1",
				map[string]string{"app": "prod-app", "istio-locality": "region.zone"}, nil)
			addPods(t, controller, p)
			if err := waitForPod(controller, p.Status.PodIP); err != nil {
				t.Fatalf("wait for pod err: %v", err)
			}

			podServices, err = controller.GetProxyServiceInstances(&model.Proxy{
				Type:            "sidecar",
				IPAddresses:     []string{"129.0.0.2"},
				Locality:        &core.Locality{Region: "r", Zone: "z"},
				ConfigNamespace: "nsa",
				Metadata: &model.NodeMetadata{ServiceAccount: "account",
					ClusterID: clusterID,
					Labels: map[string]string{
						"app": "prod-app",
					}},
			})
			if err != nil {
				t.Fatalf("got err getting service instances")
			}

			expected = &model.ServiceInstance{

				Service: &model.Service{
					Hostname:        "svc1.nsa.svc.company.com",
					Address:         "10.0.0.1",
					Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
					ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
					Attributes: model.ServiceAttributes{
						ServiceRegistry: string(serviceregistry.Kubernetes),
						Name:            "svc1",
						Namespace:       "nsa",
						UID:             "istio://nsa/services/svc1",
						LabelSelectors:  map[string]string{"app": "prod-app"},
					},
				},
				ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
				Endpoint: &model.IstioEndpoint{
					Address:         "129.0.0.2",
					EndpointPort:    0,
					ServicePortName: "tcp-port",
					Locality: model.Locality{
						Label:     "region/zone",
						ClusterID: clusterID,
					},
					Labels:         labels.Instance{"app": "prod-app", "istio-locality": "region.zone"},
					ServiceAccount: "spiffe://cluster.local/ns/nsa/sa/svcaccount",
					TLSMode:        model.DisabledTLSModeLabel,
					UID:            "kubernetes://pod3.nsa",
				},
			}
			if len(podServices) != 1 {
				t.Fatalf("expected 1 instance, got %v", len(podServices))
			}
			if !reflect.DeepEqual(expected, podServices[0]) {
				t.Fatalf("expected instance %v, got %v", expected, podServices[0])
			}
		})
	}
}

func TestGetProxyServiceInstancesWithMultiIPsAndTargetPorts(t *testing.T) {
	pod1 := generatePod("128.0.0.1", "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	testCases := []struct {
		name    string
		pods    []*coreV1.Pod
		ips     []string
		ports   []coreV1.ServicePort
		wantNum int
	}{
		{
			name: "multiple proxy ips single port",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1", "192.168.2.6"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantNum: 2,
		},
		{
			name: "single proxy ip single port",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantNum: 1,
		},
		{
			name: "multiple proxy ips multiple ports",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1", "192.168.2.6"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
				{
					Name:       "tcp-port",
					Port:       9090,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 9090},
				},
			},
			wantNum: 4,
		},
		{
			name: "single proxy ip multiple ports same target port with different protocols",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
				{
					Name:       "http-port",
					Port:       9090,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantNum: 2,
		},
		{
			name: "single proxy ip multiple ports same target port with overlapping protocols",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
				{
					Name:       "http-7442",
					Port:       7442,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7442},
				},
				{
					Name:       "tcp-8443",
					Port:       8443,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7442},
				},
				{
					Name:       "http-7557",
					Port:       7557,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7442},
				},
			},
			wantNum: 2,
		},
		{
			name: "single proxy ip multiple ports",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
				{
					Name:       "http-port",
					Port:       9090,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 9090},
				},
			},
			wantNum: 2,
		},
	}

	for _, c := range testCases {
		for mode, name := range EndpointModeNames {
			mode := mode
			t.Run(fmt.Sprintf("%s_%s", c.name, name), func(t *testing.T) {
				// Setup kube caches
				controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: mode})
				defer controller.Stop()
				addPods(t, controller, c.pods...)
				for _, pod := range c.pods {
					if err := waitForPod(controller, pod.Status.PodIP); err != nil {
						t.Fatalf("wait for pod err: %v", err)
					}
				}

				createServiceWithTargetPorts(controller, "svc1", "nsa",
					map[string]string{
						annotation.AlphaKubernetesServiceAccounts.Name: "acct4",
						annotation.AlphaCanonicalServiceAccounts.Name:  "acctvm2@gserviceaccount2.com"},
					c.ports, map[string]string{"app": "test-app"}, t)

				ev := fx.Wait("service")
				if ev == nil {
					t.Fatal("Timeout creating service")
				}
				serviceInstances, err := controller.GetProxyServiceInstances(&model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: c.ips})
				if err != nil {
					t.Fatalf("client encountered error during GetProxyServiceInstances(): %v", err)
				}
				if len(serviceInstances) != c.wantNum {
					t.Fatalf("GetProxyServiceInstances() returned wrong # of endpoints => %d, want %d", len(serviceInstances), c.wantNum)
				}
			})
		}
	}
}

func TestController_GetIstioServiceAccounts(t *testing.T) {
	oldTrustDomain := spiffe.GetTrustDomain()
	spiffe.SetTrustDomain(domainSuffix)
	defer spiffe.SetTrustDomain(oldTrustDomain)

	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: mode})
			defer controller.Stop()

			sa1 := "acct1"
			sa2 := "acct2"
			sa3 := "acct3"
			k8sSaOnVM := "acct4"
			canonicalSaOnVM := "acctvm@gserviceaccount.com"

			pods := []*coreV1.Pod{
				generatePod("128.0.0.1", "pod1", "nsA", sa1, "node1", map[string]string{"app": "test-app"}, map[string]string{}),
				generatePod("128.0.0.2", "pod2", "nsA", sa2, "node2", map[string]string{"app": "prod-app"}, map[string]string{}),
				generatePod("128.0.0.3", "pod3", "nsB", sa3, "node1", map[string]string{"app": "prod-app"}, map[string]string{}),
			}
			addPods(t, controller, pods...)
			for _, pod := range pods {
				if err := waitForPod(controller, pod.Status.PodIP); err != nil {
					t.Fatalf("wait for pod err: %v", err)
				}
			}

			createService(controller, "svc1", "nsA",
				map[string]string{
					annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
					annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM},
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)
			fx.Wait("service")
			createService(controller, "svc2", "nsA", nil, []int32{8080}, map[string]string{"app": "staging-app"}, t)
			fx.Wait("service")

			// Endpoints are generated by Kubernetes from pod labels and service selectors.
			// Here we manually create them for mocking purpose.
			svc1Ips := []string{"128.0.0.2"}
			svc2Ips := make([]string, 0)
			portNames := []string{"tcp-port"}
			createEndpoints(controller, "svc1", "nsA", portNames, svc1Ips, nil, t)
			createEndpoints(controller, "svc2", "nsA", portNames, svc2Ips, nil, t)

			// We expect only one EDS update with Endpoints.
			<-fx.Events

			hostname := kube.ServiceHostname("svc1", "nsA", domainSuffix)
			svc, err := controller.GetService(hostname)
			if err != nil {
				t.Fatalf("failed to get service: %v", err)
			}
			sa := controller.GetIstioServiceAccounts(svc, []int{8080})
			sort.Strings(sa)
			expected := []string{
				canonicalSaOnVM,
				"spiffe://company.com/ns/nsA/sa/" + sa2,
				"spiffe://company.com/ns/nsA/sa/" + k8sSaOnVM,
			}
			if !reflect.DeepEqual(sa, expected) {
				t.Fatalf("Unexpected service accounts %v (expecting %v)", sa, expected)
			}

			hostname = kube.ServiceHostname("svc2", "nsA", domainSuffix)
			svc, err = controller.GetService(hostname)
			if err != nil {
				t.Fatalf("failed to get service: %v", err)
			}
			sa = controller.GetIstioServiceAccounts(svc, []int{})
			if len(sa) != 0 {
				t.Fatal("Failure: Expected to resolve 0 service accounts, but got: ", sa)
			}
		})
	}
}

func TestWorkloadHealthCheckInfo(t *testing.T) {
	cases := []struct {
		name     string
		pod      *coreV1.Pod
		expected model.ProbeList
	}{
		{
			"health check",
			generatePodWithProbes("128.0.0.1", "pod1", "nsa1", "", "node1", "/ready", intstr.Parse("8080"), "/live", intstr.Parse("9090")),
			model.ProbeList{
				{
					Path: "/ready",
					Port: &model.Port{
						Name:     "mgmt-8080",
						Port:     8080,
						Protocol: protocol.HTTP,
					},
				},
				{
					Path: "/live",
					Port: &model.Port{
						Name:     "mgmt-9090",
						Port:     9090,
						Protocol: protocol.HTTP,
					},
				},
			},
		},
		{
			"prometheus scrape",
			generatePod("128.0.0.1", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
				map[string]string{PrometheusScrape: "true"}),
			model.ProbeList{{
				Path: PrometheusPathDefault,
			}},
		},
		{
			"prometheus path",
			generatePod("128.0.0.1", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
				map[string]string{PrometheusScrape: "true", PrometheusPath: "/other"}),
			model.ProbeList{{
				Path: "/other",
			}},
		},
		{
			"prometheus port",
			generatePod("128.0.0.1", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
				map[string]string{PrometheusScrape: "true", PrometheusPort: "3210"}),
			model.ProbeList{{
				Port: &model.Port{
					Port: 3210,
				},
				Path: PrometheusPathDefault,
			}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			controller, _ := newFakeControllerWithOptions(fakeControllerOptions{mode: EndpointsOnly})
			defer controller.Stop()

			addPods(t, controller, tt.pod)
			if err := waitForPod(controller, tt.pod.Status.PodIP); err != nil {
				t.Fatalf("wait for pod err: %v", err)
			}

			probes := controller.WorkloadHealthCheckInfo("128.0.0.1")

			if len(probes) != len(tt.expected) {
				t.Fatalf("Expecting 1 probe but got %d\r\n", len(probes))
			}
			if !reflect.DeepEqual(tt.expected, probes) {
				t.Fatalf("Probe got:\n%#v\nwanted:\n%#v\n", probes, tt.expected)
			}
		})
	}
}

func TestManagementPorts(t *testing.T) {
	controller, _ := newFakeControllerWithOptions(fakeControllerOptions{mode: EndpointsOnly})
	defer controller.Stop()

	pod := generatePodWithProbes("128.0.0.1", "pod1", "nsA", "", "node1", "/ready", intstr.Parse("8080"), "/live", intstr.Parse("9090"))
	addPods(t, controller, pod)
	if err := waitForPod(controller, pod.Status.PodIP); err != nil {
		t.Fatalf("wait for pod err: %v", err)
	}
	controller.pods.podsByIP["128.0.0.1"] = "nsA/pod1"

	portList := controller.ManagementPorts("128.0.0.1")

	expected := model.PortList{
		{
			Name:     "mgmt-8080",
			Port:     8080,
			Protocol: protocol.HTTP,
		},
		{
			Name:     "mgmt-9090",
			Port:     9090,
			Protocol: protocol.HTTP,
		},
	}

	if len(portList) != len(expected) {
		t.Fatalf("Expecting %d port but got %d\r\n", len(expected), len(portList))
	}

	if !reflect.DeepEqual(expected, portList) {
		t.Fatalf("got port, got:\n%#v\nwanted:\n%#v\n", portList, expected)
	}
}

func TestController_Service(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: mode})
			defer controller.Stop()
			// Use a timeout to keep the test from hanging.

			createService(controller, "svc1", "nsA",
				map[string]string{},
				[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
			<-fx.Events
			createService(controller, "svc2", "nsA",
				map[string]string{},
				[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
			<-fx.Events
			createService(controller, "svc3", "nsA",
				map[string]string{},
				[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
			<-fx.Events
			createService(controller, "svc4", "nsA",
				map[string]string{},
				[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)
			<-fx.Events

			expectedSvcList := []*model.Service{
				{
					Hostname: kube.ServiceHostname("svc1", "nsA", domainSuffix),
					Address:  "10.0.0.1",
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8080,
							Protocol: protocol.TCP,
						},
					},
				},
				{
					Hostname: kube.ServiceHostname("svc2", "nsA", domainSuffix),
					Address:  "10.0.0.1",
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8081,
							Protocol: protocol.TCP,
						},
					},
				},
				{
					Hostname: kube.ServiceHostname("svc3", "nsA", domainSuffix),
					Address:  "10.0.0.1",
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8082,
							Protocol: protocol.TCP,
						},
					},
				},
				{
					Hostname: kube.ServiceHostname("svc4", "nsA", domainSuffix),
					Address:  "10.0.0.1",
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8083,
							Protocol: protocol.TCP,
						},
					},
				},
			}

			svcList, _ := controller.Services()
			if len(svcList) != len(expectedSvcList) {
				t.Fatalf("Expecting %d service but got %d\r\n", len(expectedSvcList), len(svcList))
			}
			for i, exp := range expectedSvcList {
				if exp.Hostname != svcList[i].Hostname {
					t.Fatalf("got hostname of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Hostname, exp.Hostname)
				}
				if exp.Address != svcList[i].Address {
					t.Fatalf("got address of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Address, exp.Address)
				}
				if !reflect.DeepEqual(exp.Ports, svcList[i].Ports) {
					t.Fatalf("got ports of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Ports, exp.Ports)
				}
			}
		})
	}
}

func TestExternalNameServiceInstances(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: mode})
			defer controller.Stop()
			createExternalNameService(controller, "svc5", "nsA",
				[]int32{1, 2, 3}, "foo.co", t, fx.Events)

			converted, err := controller.Services()
			if err != nil || len(converted) != 1 {
				t.Fatalf("failed to get services (%v): %v", converted, err)
			}
			instances, err := controller.InstancesByPort(converted[0], 1, labels.Collection{})
			if err != nil {
				t.Fatal(err)
			}
			if len(instances) != 1 {
				t.Fatalf("expected 1 instance, got %v", instances)
			}
			if instances[0].ServicePort.Port != 1 {
				t.Fatalf("expected port 1, got %v", instances[0].ServicePort.Port)
			}
		})
	}
}

func TestController_ExternalNameService(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			deleteWg := sync.WaitGroup{}
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{
				mode: mode,
				serviceHandler: func(_ *model.Service, e model.Event) {
					if e == model.EventDelete {
						deleteWg.Done()
					}
				},
			})
			defer controller.Stop()
			// Use a timeout to keep the test from hanging.

			k8sSvcs := []*coreV1.Service{
				createExternalNameService(controller, "svc1", "nsA",
					[]int32{8080}, "test-app-1.test.svc."+domainSuffix, t, fx.Events),
				createExternalNameService(controller, "svc2", "nsA",
					[]int32{8081}, "test-app-2.test.svc."+domainSuffix, t, fx.Events),
				createExternalNameService(controller, "svc3", "nsA",
					[]int32{8082}, "test-app-3.test.pod."+domainSuffix, t, fx.Events),
				createExternalNameService(controller, "svc4", "nsA",
					[]int32{8083}, "g.co", t, fx.Events),
			}

			expectedSvcList := []*model.Service{
				{
					Hostname: kube.ServiceHostname("svc1", "nsA", domainSuffix),
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8080,
							Protocol: protocol.TCP,
						},
					},
					MeshExternal: true,
					Resolution:   model.DNSLB,
				},
				{
					Hostname: kube.ServiceHostname("svc2", "nsA", domainSuffix),
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8081,
							Protocol: protocol.TCP,
						},
					},
					MeshExternal: true,
					Resolution:   model.DNSLB,
				},
				{
					Hostname: kube.ServiceHostname("svc3", "nsA", domainSuffix),
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8082,
							Protocol: protocol.TCP,
						},
					},
					MeshExternal: true,
					Resolution:   model.DNSLB,
				},
				{
					Hostname: kube.ServiceHostname("svc4", "nsA", domainSuffix),
					Ports: model.PortList{
						&model.Port{
							Name:     "tcp-port",
							Port:     8083,
							Protocol: protocol.TCP,
						},
					},
					MeshExternal: true,
					Resolution:   model.DNSLB,
				},
			}

			svcList, _ := controller.Services()
			if len(svcList) != len(expectedSvcList) {
				t.Fatalf("Expecting %d service but got %d\r\n", len(expectedSvcList), len(svcList))
			}
			for i, exp := range expectedSvcList {
				if exp.Hostname != svcList[i].Hostname {
					t.Fatalf("got hostname of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Hostname, exp.Hostname)
				}
				if !reflect.DeepEqual(exp.Ports, svcList[i].Ports) {
					t.Fatalf("got ports of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Ports, exp.Ports)
				}
				if svcList[i].MeshExternal != exp.MeshExternal {
					t.Fatalf("i=%v, MeshExternal==%v, should be %v: externalName='%s'", i, exp.MeshExternal, svcList[i].MeshExternal, k8sSvcs[i].Spec.ExternalName)
				}
				if svcList[i].Resolution != exp.Resolution {
					t.Fatalf("i=%v, Resolution=='%v', should be '%v'", i, svcList[i].Resolution, exp.Resolution)
				}
				instances, err := controller.InstancesByPort(svcList[i], svcList[i].Ports[0].Port, labels.Collection{})
				if err != nil {
					t.Fatalf("error getting instances by port: %s", err)
				}
				if len(instances) != 1 {
					t.Fatalf("should be exactly 1 instance: len(instances) = %v", len(instances))
				}
				if instances[0].Endpoint.Address != k8sSvcs[i].Spec.ExternalName {
					t.Fatalf("wrong instance endpoint address: '%s' != '%s'", instances[0].Endpoint.Address, k8sSvcs[i].Spec.ExternalName)
				}
			}

			deleteWg.Add(len(k8sSvcs))
			for _, s := range k8sSvcs {
				deleteExternalNameService(controller, s.Name, s.Namespace, t, fx.Events)
			}
			deleteWg.Wait()

			svcList, _ = controller.Services()
			if len(svcList) != 0 {
				t.Fatalf("Should have 0 services at this point")
			}
			for _, exp := range expectedSvcList {
				instances, err := controller.InstancesByPort(exp, exp.Ports[0].Port, labels.Collection{})
				if err != nil {
					t.Fatalf("error getting instances by port: %s", err)
				}
				if len(instances) != 0 {
					t.Fatalf("should be exactly 0 instance: len(instances) = %v", len(instances))
				}
			}
		})
	}
}

func TestCompareEndpoints(t *testing.T) {
	addressA := coreV1.EndpointAddress{IP: "1.2.3.4", Hostname: "a"}
	addressB := coreV1.EndpointAddress{IP: "1.2.3.4", Hostname: "b"}
	portA := coreV1.EndpointPort{Name: "a"}
	portB := coreV1.EndpointPort{Name: "b"}
	cases := []struct {
		name string
		a    *coreV1.Endpoints
		b    *coreV1.Endpoints
		want bool
	}{
		{"both empty", &coreV1.Endpoints{}, &coreV1.Endpoints{}, true},
		{
			"just not ready endpoints",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{NotReadyAddresses: []coreV1.EndpointAddress{addressA}},
			}},
			&coreV1.Endpoints{},
			false,
		},
		{
			"not ready to ready",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{NotReadyAddresses: []coreV1.EndpointAddress{addressA}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}},
			}},
			false,
		},
		{
			"ready and not ready address",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{
					NotReadyAddresses: []coreV1.EndpointAddress{addressB},
					Addresses:         []coreV1.EndpointAddress{addressA},
				},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}},
			}},
			true,
		},
		{
			"different addresses",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressB}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}},
			}},
			false,
		},
		{
			"different ports",
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{portA}},
			}},
			&coreV1.Endpoints{Subsets: []coreV1.EndpointSubset{
				{Addresses: []coreV1.EndpointAddress{addressA}, Ports: []coreV1.EndpointPort{portB}},
			}},
			false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := compareEndpoints(tt.a, tt.b)
			inverse := compareEndpoints(tt.b, tt.a)
			if got != tt.want {
				t.Fatalf("Compare endpoints got %v, want %v", got, tt.want)
			}
			if got != inverse {
				t.Fatalf("Expected to be commutative, but was not")
			}
		})
	}
}

func createEndpoints(controller *Controller, name, namespace string, portNames, ips []string, refs []*coreV1.ObjectReference, t *testing.T) {
	if refs == nil {
		refs = make([]*coreV1.ObjectReference, len(ips))
	}
	var portNum int32 = 1001
	eas := make([]coreV1.EndpointAddress, 0)
	for i, ip := range ips {
		eas = append(eas, coreV1.EndpointAddress{IP: ip, TargetRef: refs[i]})
	}

	eps := make([]coreV1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, coreV1.EndpointPort{Name: name, Port: portNum})
	}

	endpoint := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []coreV1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if _, err := controller.client.CoreV1().Endpoints(namespace).Create(context.TODO(), endpoint, metaV1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = controller.client.CoreV1().Endpoints(namespace).Update(context.TODO(), endpoint, metaV1.UpdateOptions{})
		}
		if err != nil {
			t.Fatalf("failed to create endpoints %s in namespace %s (error %v)", name, namespace, err)
		}
	}

	// Create endpoint slice as well
	esps := make([]discoveryv1alpha1.EndpointPort, 0)
	for _, name := range portNames {
		n := name // Create a stable reference to take the pointer from
		esps = append(esps, discoveryv1alpha1.EndpointPort{Name: &n, Port: &portNum})
	}

	sliceEndpoint := []discoveryv1alpha1.Endpoint{}
	for i, ip := range ips {
		sliceEndpoint = append(sliceEndpoint, discoveryv1alpha1.Endpoint{
			Addresses: []string{ip},
			TargetRef: refs[i],
		})
	}
	endpointSlice := &discoveryv1alpha1.EndpointSlice{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1alpha1.LabelServiceName: name,
			},
		},
		Endpoints: sliceEndpoint,
		Ports:     esps,
	}
	if _, err := controller.client.DiscoveryV1alpha1().EndpointSlices(namespace).Create(context.TODO(), endpointSlice, metaV1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = controller.client.DiscoveryV1alpha1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metaV1.UpdateOptions{})
		}
		if err != nil {
			t.Fatalf("failed to create endpoint slice %s in namespace %s (error %v)", name, namespace, err)
		}
	}

}

func updateEndpoints(controller *Controller, name, namespace string, portNames, ips []string, t *testing.T) {
	var portNum int32 = 1001
	eas := make([]coreV1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, coreV1.EndpointAddress{IP: ip})
	}

	eps := make([]coreV1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, coreV1.EndpointPort{Name: name, Port: portNum})
	}

	endpoint := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []coreV1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if _, err := controller.client.CoreV1().Endpoints(namespace).Update(context.TODO(), endpoint, metaV1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update endpoints %s in namespace %s (error %v)", name, namespace, err)
	}

	// Update endpoint slice as well
	esps := make([]discoveryv1alpha1.EndpointPort, 0)
	for _, name := range portNames {
		esps = append(esps, discoveryv1alpha1.EndpointPort{Name: &name, Port: &portNum})
	}
	endpointSlice := &discoveryv1alpha1.EndpointSlice{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1alpha1.LabelServiceName: name,
			},
		},
		Endpoints: []discoveryv1alpha1.Endpoint{
			{
				Addresses: ips,
			},
		},
		Ports: esps,
	}
	if _, err := controller.client.DiscoveryV1alpha1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metaV1.UpdateOptions{}); err != nil {
		t.Errorf("failed to create endpoint slice %s in namespace %s (error %v)", name, namespace, err)
	}
}

func createServiceWithTargetPorts(controller *Controller, name, namespace string, annotations map[string]string,
	svcPorts []coreV1.ServicePort, selector map[string]string, t *testing.T) {
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: coreV1.ServiceSpec{
			ClusterIP: "10.0.0.1", // FIXME: generate?
			Ports:     svcPorts,
			Selector:  selector,
			Type:      coreV1.ServiceTypeClusterIP,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func createService(controller *Controller, name, namespace string, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T) {

	svcPorts := make([]coreV1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, coreV1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: coreV1.ServiceSpec{
			ClusterIP: "10.0.0.1", // FIXME: generate?
			Ports:     svcPorts,
			Selector:  selector,
			Type:      coreV1.ServiceTypeClusterIP,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func createServiceWithoutClusterIP(controller *Controller, name, namespace string, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T) {

	svcPorts := make([]coreV1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, coreV1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: coreV1.ServiceSpec{
			ClusterIP: coreV1.ClusterIPNone,
			Ports:     svcPorts,
			Selector:  selector,
			Type:      coreV1.ServiceTypeClusterIP,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

// nolint: unparam
func createExternalNameService(controller *Controller, name, namespace string,
	ports []int32, externalName string, t *testing.T, xdsEvents <-chan XdsEvent) *coreV1.Service {

	defer func() {
		<-xdsEvents
	}()

	svcPorts := make([]coreV1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, coreV1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coreV1.ServiceSpec{
			Ports:        svcPorts,
			Type:         coreV1.ServiceTypeExternalName,
			ExternalName: externalName,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
	return service
}

func deleteExternalNameService(controller *Controller, name, namespace string, t *testing.T, xdsEvents <-chan XdsEvent) {

	defer func() {
		<-xdsEvents
	}()

	err := controller.client.CoreV1().Services(namespace).Delete(context.TODO(), name, metaV1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Cannot delete service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func addPods(t *testing.T, controller *Controller, pods ...*coreV1.Pod) {
	for _, pod := range pods {
		p, _ := controller.client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metaV1.GetOptions{})
		var newPod *coreV1.Pod
		var err error
		if p == nil {
			newPod, err = controller.client.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metaV1.CreateOptions{})
			if err != nil {
				t.Fatalf("Cannot create %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
			}
		} else {
			newPod, err = controller.client.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metaV1.UpdateOptions{})
			if err != nil {
				t.Fatalf("Cannot update %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
			}
		}
		// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
		// events - since PodIP will be "".
		newPod.Status.PodIP = pod.Status.PodIP
		newPod.Status.Phase = coreV1.PodRunning
		_, _ = controller.client.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metaV1.UpdateOptions{})
	}
}

func generatePod(ip, name, namespace, saName, node string, labels map[string]string, annotations map[string]string) *coreV1.Pod {
	automount := false
	return &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: coreV1.PodSpec{
			ServiceAccountName:           saName,
			NodeName:                     node,
			AutomountServiceAccountToken: &automount,
			// Validation requires this
			Containers: []coreV1.Container{
				{
					Name:  "test",
					Image: "ununtu",
				},
			},
		},
		// The cache controller uses this as key, required by our impl.
		Status: coreV1.PodStatus{
			PodIP:  ip,
			HostIP: ip,
			Phase:  coreV1.PodRunning,
		},
	}
}

func generatePodWithProbes(ip, name, namespace, saName, node string, readinessPath string, readinessPort intstr.IntOrString,
	livenessPath string, livenessPort intstr.IntOrString) *coreV1.Pod {
	return &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coreV1.PodSpec{
			ServiceAccountName: saName,
			NodeName:           node,
			Containers: []coreV1.Container{{
				ReadinessProbe: &coreV1.Probe{
					Handler: coreV1.Handler{
						HTTPGet: &coreV1.HTTPGetAction{
							Path: readinessPath,
							Port: readinessPort,
						},
					},
				},
				LivenessProbe: &coreV1.Probe{
					Handler: coreV1.Handler{
						HTTPGet: &coreV1.HTTPGetAction{
							Path: livenessPath,
							Port: livenessPort,
						},
					},
				},
			}},
		},
		// The cache controller uses this as key, required by our impl.
		Status: coreV1.PodStatus{
			PodIP:  ip,
			HostIP: ip,
			Phase:  coreV1.PodRunning,
		},
	}
}

func generateNode(name string, labels map[string]string) *coreV1.Node {
	return &coreV1.Node{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func addNodes(t *testing.T, controller *Controller, nodes ...*coreV1.Node) {
	nodeResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}
	fakeClient := controller.metadataClient.(*metafake.FakeMetadataClient)

	for _, node := range nodes {
		partialMetadata := &metaV1.PartialObjectMetadata{
			TypeMeta:   node.TypeMeta,
			ObjectMeta: node.ObjectMeta,
		}
		_, err := fakeClient.Resource(nodeResource).(metafake.MetadataClient).CreateFake(partialMetadata, metaV1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}

}

func TestEndpointUpdate(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: mode})
			defer controller.Stop()

			pod1 := generatePod("128.0.0.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
			pods := []*coreV1.Pod{pod1}
			addPods(t, controller, pods...)
			for _, pod := range pods {
				if err := waitForPod(controller, pod.Status.PodIP); err != nil {
					t.Fatalf("wait for pod err: %v", err)
				}
				// pod first time occur will trigger proxy push
				if ev := fx.Wait("proxy"); ev == nil {
					t.Fatal("Timeout creating service")
				}
			}

			// 1. incremental eds for normal service endpoint update
			createService(controller, "svc1", "nsa", nil,
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}

			// Endpoints are generated by Kubernetes from pod labels and service selectors.
			// Here we manually create them for mocking purpose.
			svc1Ips := []string{"128.0.0.1"}
			portNames := []string{"tcp-port"}
			// Create 1 endpoint that refers to a pod in the same namespace.
			createEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, nil, t)
			if ev := fx.Wait("eds"); ev == nil {
				t.Fatalf("Timeout incremental eds")
			}

			// delete normal service
			err := controller.client.CoreV1().Services("nsa").Delete(context.TODO(), "svc1", metaV1.DeleteOptions{})
			if err != nil {
				t.Fatalf("Cannot delete service (error: %v)", err)
			}
			if ev := fx.Wait("service"); ev == nil {
				t.Fatalf("Timeout deleting service")
			}

			// 2. full xds push request for headless service endpoint update

			// create a headless service
			createServiceWithoutClusterIP(controller, "svc1", "nsa", nil,
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatalf("Timeout creating service")
			}

			// Create 1 endpoint that refers to a pod in the same namespace.
			svc1Ips = append(svc1Ips, "128.0.0.2")
			updateEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, t)
			if ev := fx.Wait("xds"); ev == nil {
				t.Fatalf("Timeout xds push")
			}
		})
	}
}

// Validates that when Pilot sees Endpoint before the corresponding Pod, it triggers endpoint event on pod event.
func TestEndpointUpdateBeforePodUpdate(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := newFakeControllerWithOptions(fakeControllerOptions{mode: mode})
			// Setup kube caches
			defer controller.Stop()
			addNodes(t, controller, generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", IstioSubzoneLabel: "subzone1"}))
			// Setup help functions to make the test more explicit
			addPod := func(name, ip string) {
				pod := generatePod(ip, name, "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
				addPods(t, controller, pod)
				if err := waitForPod(controller, pod.Status.PodIP); err != nil {
					t.Fatalf("wait for pod err: %v", err)
				}
				// pod first time occur will trigger proxy push
				if ev := fx.Wait("proxy"); ev == nil {
					t.Fatal("Timeout creating pod")
				}
			}
			deletePod := func(name, ip string) {
				if err := controller.client.CoreV1().Pods("nsA").Delete(context.TODO(), name, metaV1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				retry.UntilSuccessOrFail(t, func() error {
					controller.pods.RLock()
					defer controller.pods.RUnlock()
					if _, ok := controller.pods.podsByIP[ip]; ok {
						return fmt.Errorf("pod still present")
					}
					return nil
				}, retry.Timeout(time.Second))
			}
			addService := func(name string) {
				// create service
				createService(controller, name, "nsA", nil,
					[]int32{8080}, map[string]string{"app": "prod-app"}, t)
				if ev := fx.Wait("service"); ev == nil {
					t.Fatal("Timeout creating service")
				}

			}
			addEndpoint := func(svcName string, ips []string, pods []string) {
				refs := []*coreV1.ObjectReference{}
				for _, pod := range pods {
					if pod == "" {
						refs = append(refs, nil)
					} else {
						refs = append(refs, &coreV1.ObjectReference{
							Kind:      "Pod",
							Namespace: "nsA",
							Name:      pod,
						})
					}
				}
				createEndpoints(controller, svcName, "nsA", []string{"tcp-port"}, ips, refs, t)
			}
			assertEndpointsEvent := func(expected ...string) {
				t.Helper()
				ev := fx.Wait("eds")
				if ev == nil {
					t.Fatalf("Timeout eds")
				}
				ips := []string{}
				for _, e := range ev.Endpoints {
					ips = append(ips, e.Address)
				}
				if !reflect.DeepEqual(expected, ips) {
					t.Fatalf("expected ips %v, got %v", expected, ips)
				}
			}
			assertPendingResync := func(expected int) {
				t.Helper()
				retry.UntilSuccessOrFail(t, func() error {
					controller.pods.RLock()
					defer controller.pods.RUnlock()
					if len(controller.pods.needResync) != expected {
						return fmt.Errorf("expected %d pods needing resync, got %d", expected, len(controller.pods.needResync))
					}
					return nil
				}, retry.Timeout(time.Second))
			}

			// standard ordering
			addService("svc")
			addPod("pod1", "172.0.1.1")
			addEndpoint("svc", []string{"172.0.1.1"}, []string{"pod1"})
			assertEndpointsEvent("172.0.1.1")
			fx.Clear()

			// Create the endpoint, then later add the pod. Should eventually get an update for the endpoint
			addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
			assertEndpointsEvent("172.0.1.1")
			fx.Clear()
			addPod("pod2", "172.0.1.2")
			assertEndpointsEvent("172.0.1.1", "172.0.1.2")
			fx.Clear()

			// Create the endpoint without a pod reference. We should see it immediately
			addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", ""})
			assertEndpointsEvent("172.0.1.1", "172.0.1.2", "172.0.1.3")
			fx.Clear()

			// Delete a pod before the endpoint
			addEndpoint("svc", []string{"172.0.1.1"}, []string{"pod1"})
			deletePod("pod2", "172.0.1.2")
			assertEndpointsEvent("172.0.1.1")
			fx.Clear()

			// add another service
			addService("other")
			// Add endpoints for the new service, and the old one. Both should be missing the last IP
			addEndpoint("other", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
			addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
			assertEndpointsEvent("172.0.1.1")
			assertEndpointsEvent("172.0.1.1")
			fx.Clear()
			// Add the pod, expect the endpoints update for both
			addPod("pod2", "172.0.1.2")
			assertEndpointsEvent("172.0.1.1", "172.0.1.2")
			assertEndpointsEvent("172.0.1.1", "172.0.1.2")

			// Check for memory leaks
			assertPendingResync(0)
			addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", "pod3"})
			// This is really an implementation detail here - but checking to sanity check our test
			assertPendingResync(1)
			// Remove the endpoint again, with no pod events in between. Should have no memory leaks
			addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
			// TODO this case would leak
			//assertPendingResync(0)

			// completely remove the endpoint
			addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", "pod3"})
			assertPendingResync(1)
			if err := controller.client.CoreV1().Endpoints("nsA").Delete(context.TODO(), "svc", metaV1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := controller.client.DiscoveryV1alpha1().EndpointSlices("nsA").Delete(context.TODO(), "svc", metaV1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			assertPendingResync(0)
		})
	}
}
