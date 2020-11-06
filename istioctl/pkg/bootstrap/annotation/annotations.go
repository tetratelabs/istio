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

package annotation

const (
	// Name of the Kubernetes config map that holds the root cert of a k8s CA.
	//
	// By default, config map is considered undefined and thus the only way to find out
	// the root cert of a k8s CA is
	//  1) either to read a k8s Secret with a ServiceAccountToken, which among other things
	//     holds the root cert of a k8s CA
	//  2) or to read the root cert of a k8s CA from the `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`
	//     file, which is auto-mounted into Pods by k8s
	K8sCaRootCertConfigMapName = "sidecar-bootstrap.istioctl.istio.io/k8s-ca-root-cert-configmap"

	// Name of the Kubernetes config map that holds configuration intended for those
	// Istio Proxies that expand the mesh.
	//
	// This configuration is applied on top of mesh-wide default ProxyConfig,
	// but prior to the workload-specific ProxyConfig from `proxy.istio.io/config` annotation
	// on a WorkloadEntry.
	//
	// By default, config map is considered undefined and thus expansion proxies will
	// have the same configuration as the regular ones.
	MeshExpansionConfigMapName = "sidecar-bootstrap.istioctl.istio.io/mesh-expansion-configmap"

	// IP address or DNS name of the machine represented by this WorkloadEntry to use
	// instead of WorkloadEntry.Address for SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// This setting is intended for those scenarios where `istioctl x sidecar-bootstrap`
	// will be run on a machine without direct connectivity to the WorkloadEntry.Address.
	// E.g., one might set WorkloadEntry.Address to the "internal IP" of a VM
	// and set value of this annotation to the "external IP" of that VM.
	//
	// By default, value of WorkloadEntry.Address is assumed.
	SshHost = "sidecar-bootstrap.istioctl.istio.io/ssh-host"

	// Port of the SSH server on the machine represented by this WorkloadEntry to use
	// for SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// By default, `22` is assumed.
	SshPort = "sidecar-bootstrap.istioctl.istio.io/ssh-port"

	// User on the machine represented by this WorkloadEntry to use for SSH connections
	// from `istioctl x sidecar-bootstrap`.
	//
	// Make sure that user has enough permissions to create the config dir and
	// to run Docker container without `sudo`.
	//
	// By default, a user running `istioctl x sidecar-bootstrap` is assumed.
	SshUser = "sidecar-bootstrap.istioctl.istio.io/ssh-user"

	// Path to the `scp` binary on the machine represented by this WorkloadEntry to use
	// in SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// By default, `/usr/bin/scp` is assumed.
	ScpPath = "sidecar-bootstrap.istioctl.istio.io/scp-path"

	// Directory on the machine represented by this WorkloadEntry where `istioctl x sidecar-bootstrap`
	// should copy bootstrap bundle to.
	//
	// By default, `/tmp/istio-proxy` is assumed (the most reliable default value for out-of-the-box experience).
	ProxyConfigDir = "sidecar-bootstrap.istioctl.istio.io/proxy-config-dir"

	// Hub with Istio Proxy images that the machine represented by this WorkloadEntry
	// should pull from instead of the mesh-wide hub.
	//
	// By default, mesh-wide hub is assumed.
	ProxyImageHub = "sidecar-bootstrap.istioctl.istio.io/proxy-image-hub"

	// IP address of the machine represented by this WorkloadEntry that Istio Proxy
	// should bind `inbound` listeners to.
	//
	// This setting is intended for those scenarios where Istio Proxy cannot bind to
	// the IP address specified in the WorkloadEntry.Address (e.g., on AWS EC2 where
	// a VM can only bind the private IP but not the public one).
	//
	// By default, WorkloadEntry.Address is assumed.
	ProxyInstanceIP = "sidecar-bootstrap.istioctl.istio.io/proxy-instance-ip"
)
