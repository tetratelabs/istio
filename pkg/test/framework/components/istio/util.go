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

package istio

import (
	"context"
	"fmt"
	"net"
	"time"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	dummyValidationRuleTemplate = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: validation-readiness-dummy-rule
  namespace: %s
spec:
  match: request.headers["foo"] == "bar"
  actions:
  - handler: validation-readiness-dummy
    instances:
    - validation-readiness-dummy
`
)

var (
	igwServiceName = "istio-ingressgateway"
	discoveryPort  = 15012
)

func waitForValidationWebhook(ctx resource.Context, cluster resource.Cluster, cfg Config) error {
	dummyValidationRule := fmt.Sprintf(dummyValidationRuleTemplate, cfg.SystemNamespace)
	defer func() {
		e := ctx.Config(cluster).DeleteYAML("", dummyValidationRule)
		if e != nil {
			scopes.Framework.Warnf("error deleting dummy rule for waiting the validation webhook: %v", e)
		}
	}()

	scopes.Framework.Info("Creating dummy rule to check for validation webhook readiness")
	return retry.UntilSuccess(func() error {
		err := ctx.Config(cluster).ApplyYAML("", dummyValidationRule)
		if err == nil {
			return nil
		}

		return fmt.Errorf("validation webhook not ready yet: %v", err)
	}, retry.Timeout(time.Minute))
}

func GetRemoteDiscoveryAddress(namespace string, cluster resource.Cluster, useNodePort bool) (net.TCPAddr, error) {
	svc, err := cluster.CoreV1().Services(namespace).Get(context.TODO(), igwServiceName, kubeApiMeta.GetOptions{})
	if err != nil {
		return net.TCPAddr{}, err
	}

	// if useNodePort is set, we look for the node port service. This is generally used on kind or k8s without a LB
	// and that do not have metallb installed
	if useNodePort {
		pods, err := cluster.PodsForSelector(context.TODO(), namespace, "istio=ingressgateway")
		if err != nil {
			return net.TCPAddr{}, err
		}
		if len(pods.Items) == 0 {
			return net.TCPAddr{}, fmt.Errorf("no ingress pod found")
		}
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return net.TCPAddr{}, fmt.Errorf("no Host IP available on the ingress node yet")
		}
		if len(svc.Spec.Ports) == 0 {
			return net.TCPAddr{}, fmt.Errorf("no ports found in service istio-ingressgateway")
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == int32(discoveryPort) {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return net.TCPAddr{}, fmt.Errorf("no port found in service: istio-ingressgateway")
		}
		return net.TCPAddr{IP: net.ParseIP(ip), Port: int(nodePort)}, nil
	}

	// If running in KinD, MetalLB must be installed to enable LoadBalancer resources
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return net.TCPAddr{}, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: discoveryPort}, nil
}

func getRemoteServiceAddress(s *kube.Settings, cluster resource.Cluster, ns, label, svcName string,
	port int) (interface{}, bool, error) {

	if s.Minikube {
		pods, err := cluster.PodsForSelector(context.TODO(), ns, fmt.Sprintf("istio=%s", label))
		if err != nil {
			return nil, false, err
		}

		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		scopes.Framework.Debugf("Querying remote service, pods:%v", names)
		if len(pods.Items) == 0 {
			return nil, false, fmt.Errorf("no remote service pod found")
		}

		scopes.Framework.Debugf("Found pod: %v", pods.Items[0].Name)
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the remote service node yet")
		}

		svc, err := cluster.CoreV1().Services(ns).Get(context.TODO(), svcName, kubeApiMeta.GetOptions{})
		if err != nil {
			return nil, false, err
		}

		if len(svc.Spec.Ports) == 0 {
			return nil, false, fmt.Errorf("no ports found in service: %s/%s", ns, svcName)
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == int32(port) {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return nil, false, fmt.Errorf("no port %d found in service: %s/%s", port, ns, svcName)
		}

		return net.TCPAddr{IP: net.ParseIP(ip), Port: int(nodePort)}, true, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := cluster.CoreV1().Services(ns).Get(context.TODO(), svcName, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service %s is not available yet: %s/%s", svcName, svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: port}, true, nil
}
