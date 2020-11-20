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

import (
	"istio.io/api/annotation"
)

type Instance = annotation.Instance

var (
	K8sCaRootCertConfigMapName = Instance{
		Name: "sidecar-bootstrap.istio.io/k8s-ca-root-cert-configmap",
		Description: `Name of the Kubernetes config map that holds the root cert of a k8s CA.

By default, config map is considered undefined and thus the only way to find out
the root cert of a k8s CA is
1) either to read a k8s Secret with a ServiceAccountToken, which among other things
   holds the root cert of a k8s CA
2) or to read the root cert of a k8s CA from the "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
   file, which is auto-mounted into Pods by k8s`,
	}

	MeshExpansionConfigMapName = Instance{
		Name: "sidecar-bootstrap.istio.io/mesh-expansion-configmap",
		Description: `Name of the Kubernetes config map that holds configuration intended for those
Istio Proxies that expand the mesh.

This configuration is applied on top of mesh-wide default ProxyConfig,
but prior to the workload-specific ProxyConfig from "proxy.istio.io/config" annotation
on a WorkloadEntry.

By default, config map is considered undefined and thus expansion proxies will
have the same configuration as the regular ones.`,
	}

	SSHHost = Instance{
		Name: "sidecar-bootstrap.istio.io/ssh-host",
		Description: `IP address or DNS name of the machine represented by this WorkloadEntry to use
instead of WorkloadEntry.Address for SSH connections initiated by the "sidecar-bootstrap" command.

This setting is intended for those scenarios where "sidecar-bootstrap" command
will be run on a machine without direct connectivity to the WorkloadEntry.Address.
E.g., one might set WorkloadEntry.Address to the "internal IP" of a VM
and set value of this annotation to the "external IP" of that VM.

By default, value of WorkloadEntry.Address is assumed.`,
	}

	SSHPort = Instance{
		Name: "sidecar-bootstrap.istio.io/ssh-port",
		Description: `Port of the SSH server on the machine represented by this WorkloadEntry to use
for SSH connections initiated by the "sidecar-bootstrap" command.

By default, "22" is assumed.`,
	}

	SSHUser = Instance{
		Name: "sidecar-bootstrap.istio.io/ssh-user",
		Description: `User on the machine represented by this WorkloadEntry to use for SSH connections
initiated by the "sidecar-bootstrap" command.

Make sure that user has enough permissions to create the config dir and
to run Docker container without "sudo".

By default, a user running "sidecar-bootstrap" command is assumed.`,
	}

	ScpPath = Instance{
		Name: "sidecar-bootstrap.istio.io/scp-path",
		Description: `Path to the "scp" binary on the machine represented by this WorkloadEntry to use
in SSH connections initiated by the "sidecar-bootstrap" command.

By default, "/usr/bin/scp" is assumed.`,
	}

	ProxyConfigDir = Instance{
		Name: "sidecar-bootstrap.istio.io/proxy-config-dir",
		Description: `Directory on the machine represented by this WorkloadEntry where "sidecar-bootstrap" command
should copy bootstrap bundle to.

By default, "/tmp/istio-proxy" is assumed (the most reliable default value for out-of-the-box experience).`,
	}

	ProxyImageHub = Instance{
		Name: "sidecar-bootstrap.istio.io/proxy-image-hub",
		Description: `Hub with Istio Proxy images that the machine represented by this WorkloadEntry
should pull from instead of the mesh-wide hub.

By default, mesh-wide hub is assumed.`,
	}

	ProxyContainerName = Instance{
		Name: "sidecar-bootstrap.istio.io/proxy-container-name",
		Description: `Name for a container with Istio Proxy.

If you need to run multiple Istio Proxy containers on the same machine, make sure each of them has a unique name.

By default, "istio-proxy" is assumed.`,
	}

	ProxyInstanceIP = Instance{
		Name: "sidecar-bootstrap.istio.io/proxy-instance-ip",
		Description: `IP address of the machine represented by this WorkloadEntry that Istio Proxy
should bind "inbound" listeners to.

This setting is intended for those scenarios where Istio Proxy cannot bind to
the IP address specified in the WorkloadEntry.Address (e.g., on AWS EC2 where
a VM can only bind the private IP but not the public one).

By default, WorkloadEntry.Address is assumed.`,
	}
)

func SupportedCustomAnnotations() []*Instance {
	return []*Instance{
		&K8sCaRootCertConfigMapName,
		&MeshExpansionConfigMapName,
		&SSHHost,
		&SSHPort,
		&SSHUser,
		&ScpPath,
		&ProxyConfigDir,
		&ProxyImageHub,
		&ProxyContainerName,
		&ProxyInstanceIP,
	}
}

func SupportedIstioAnnotations() []*Instance {
	return []*Instance{
		&annotation.ProxyConfig,
		&annotation.SidecarInterceptionMode,
		&annotation.SidecarProxyImage,
		&annotation.SidecarLogLevel,
		&annotation.SidecarComponentLogLevel,
		&annotation.SidecarStatsInclusionPrefixes,
		&annotation.SidecarStatsInclusionSuffixes,
		&annotation.SidecarStatsInclusionRegexps,
	}
}
