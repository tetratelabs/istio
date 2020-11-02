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

package bundle

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	networkingCrd "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioconfig "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/util/gogoprotomarshal"

	bootstrapAnnotation "istio.io/istio/istioctl/pkg/bootstrap/annotation"
)

type SidecarData struct {
	/* k8s */
	K8sCaCert []byte
	/* mesh */
	IstioSystemNamespace       string
	IstioMeshConfig            *meshconfig.MeshConfig
	IstioConfigValues          *istioconfig.Values
	IstioCaCert                []byte
	IstioIngressGatewayAddress string
	ExpansionProxyConfig       string // optional override config for expansion proxies
	/* workload */
	Workload *networkingCrd.WorkloadEntry
	/* sidecar */
	ProxyConfig *meshconfig.ProxyConfig
}

type valueFunc func(data *SidecarData) (string, error)

type envVar struct {
	Name  string
	Value valueFunc
}

func newEnvVar(name string, fn valueFunc) envVar {
	return envVar{
		Name:  name,
		Value: fn,
	}
}

var (
	// Instruct 'istio-agent' to look for a ServiceAccount token
	// at a hardcoded path './var/run/secrets/tokens/istio-token'
	JWT_POLICY = newEnvVar("JWT_POLICY", func(data *SidecarData) (string, error) {
		return data.IstioConfigValues.GetGlobal().GetJwtPolicy(), nil
	})

	// The provider of Pilot DNS certificate setting implicitly determines
	// the path 'istio-agent' will be looking for the CA cert at:
	//  istiod:     ./var/run/secrets/istio/root-cert.pem
	//  kubernetes: ./var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	//  custom:     ./etc/certs/root-cert.pem
	PILOT_CERT_PROVIDER = newEnvVar("PILOT_CERT_PROVIDER", func(data *SidecarData) (string, error) {
		return data.IstioConfigValues.GetGlobal().GetPilotCertProvider(), nil
	})

	// If the following setting is unset, 'istio-agent' will be using it
	// implicitly in certain code paths, despite saying that it defaults to
	// XDS address.
	CA_ADDR = newEnvVar("CA_ADDR", func(data *SidecarData) (string, error) {
		return data.GetCaAddr(), nil
	})

	CA_SNI = newEnvVar("CA_SNI", func(data *SidecarData) (string, error) {
		return clusterLocalSni(data.GetCaAddr(), shortClusterLocalAlias("istiod", data.IstioSystemNamespace)), nil
	})

	PILOT_SNI = newEnvVar("PILOT_SNI", func(data *SidecarData) (string, error) {
		return clusterLocalSni(data.ProxyConfig.GetDiscoveryAddress(), shortClusterLocalAlias("istiod", data.IstioSystemNamespace)), nil
	})

	POD_NAME = newEnvVar("POD_NAME", func(data *SidecarData) (string, error) {
		addressIdentifier := addressToPodNameAddition(data.Workload.Spec.Address)
		return data.Workload.Name + "-" + addressIdentifier, nil
	})

	POD_NAMESPACE = newEnvVar("POD_NAMESPACE", func(data *SidecarData) (string, error) {
		return data.Workload.Namespace, nil
	})

	IDENTITY_IP = newEnvVar("IDENTITY_IP", func(data *SidecarData) (string, error) {
		return data.Workload.Spec.Address, nil
	})

	// Make sure that 'istio-agent' picks a given address as the primary address of this workload.
	INSTANCE_IP = newEnvVar("INSTANCE_IP", func(data *SidecarData) (string, error) {
		ip := ""
		if net.ParseIP(data.Workload.Spec.Address) != nil {
			ip = data.Workload.Spec.Address
		}
		if value := data.Workload.Annotations[bootstrapAnnotation.ProxyInstanceIP]; value != "" {
			if net.ParseIP(value) == nil {
				return "", fmt.Errorf("value of %q annotation on the WorkloadEntry is not a valid IP address: %q", bootstrapAnnotation.ProxyInstanceIP, value)
			}
			ip = value
		}
		if ip == "" {
			return "", fmt.Errorf("unable to bootstrap a WorkloadEntry that has neither an Address field set to a valid IP nor a %q annotation as an alternative source of the IP address to bind 'inbound' listeners to", bootstrapAnnotation.ProxyInstanceIP)
		}
		return ip, nil
	})

	SERVICE_ACCOUNT = newEnvVar("SERVICE_ACCOUNT", func(data *SidecarData) (string, error) {
		return data.Workload.Spec.ServiceAccount, nil
	})

	HOST_IP = newEnvVar("HOST_IP", func(data *SidecarData) (string, error) {
		return data.Workload.Spec.Address, nil
	})

	CANONICAL_SERVICE = newEnvVar("CANONICAL_SERVICE", func(data *SidecarData) (string, error) {
		return data.Workload.Labels["service.istio.io/canonical-name"], nil
	})

	CANONICAL_REVISION = newEnvVar("CANONICAL_REVISION", func(data *SidecarData) (string, error) {
		return data.Workload.Labels["service.istio.io/canonical-revision"], nil
	})

	PROXY_CONFIG = newEnvVar("PROXY_CONFIG", func(data *SidecarData) (string, error) {
		if data.ProxyConfig == nil {
			return "", nil
		}
		value, err := new(jsonpb.Marshaler).MarshalToString(data.ProxyConfig)
		if err != nil {
			return "", err
		}
		return string(value), nil
	})

	ISTIO_META_CLUSTER_ID = newEnvVar("ISTIO_META_CLUSTER_ID", func(data *SidecarData) (string, error) {
		if name := data.IstioConfigValues.GetGlobal().GetMultiCluster().GetClusterName(); name != "" {
			return name, nil
		}
		return "Kubernetes", nil
	})

	ISTIO_META_INTERCEPTION_MODE = newEnvVar("ISTIO_META_INTERCEPTION_MODE", func(data *SidecarData) (string, error) {
		if mode := data.Workload.Annotations[annotation.SidecarInterceptionMode.Name]; mode != "" {
			return mode, nil
		}
		return "NONE", nil // ignore data.ProxyConfig.GetInterceptionMode()
	})

	ISTIO_META_NETWORK = newEnvVar("ISTIO_META_NETWORK", func(data *SidecarData) (string, error) {
		if value := data.Workload.Spec.GetNetwork(); value != "" {
            return value, nil
        }
		return data.IstioConfigValues.GetGlobal().GetNetwork(), nil
	})

	// Workload labels
	ISTIO_METAJSON_LABELS = newEnvVar("ISTIO_METAJSON_LABELS", func(data *SidecarData) (string, error) {
		if len(data.Workload.Labels)+len(data.Workload.Spec.Labels) == 0 {
			return "", nil
		}
		labels := make(map[string]string)
		for name, value := range data.Workload.Labels {
			labels[name] = value
		}
		for name, value := range data.Workload.Spec.Labels {
			labels[name] = value
		}
		value, err := json.Marshal(labels)
		if err != nil {
			return "", err
		}
		return string(value), nil
	})

	// Istio-related annotations of the Workload.
	ISTIO_METAJSON_ISTIO_ANNOTATIONS = newEnvVar("ISTIO_METAJSON_ISTIO_ANNOTATIONS", func(data *SidecarData) (string, error) {
		annotations := make(map[string]string)
		for name, value := range data.Workload.Annotations {
			if strings.Contains(name, "istio.io/") && !strings.Contains(name, "istioctl.istio.io/") {
				annotations[name] = value
			}
		}
		if len(annotations) == 0 {
			return "", nil
		}
		value, err := json.Marshal(annotations)
		if err != nil {
			return "", err
		}
		return string(value), nil
	})

	ISTIO_META_WORKLOAD_NAME = newEnvVar("ISTIO_META_WORKLOAD_NAME", func(data *SidecarData) (string, error) {
		return getAppOrServiceAccount(data.Workload), nil
	})

	ISTIO_META_OWNER = newEnvVar("ISTIO_META_OWNER", func(data *SidecarData) (string, error) {
		return fmt.Sprintf("kubernetes://apis/networking.istio.io/v1alpha3/namespaces/%s/workloadentries/%s", data.Workload.Namespace, data.Workload.Name), nil
	})

	ISTIO_META_MESH_ID = newEnvVar("ISTIO_META_MESH_ID", func(data *SidecarData) (string, error) {
		if value := data.IstioConfigValues.GetGlobal().GetMeshID(); value != "" {
			return value, nil
		}
		return data.IstioConfigValues.GetGlobal().GetTrustDomain(), nil
	})

	SIDECAR_ENV = []envVar{
		JWT_POLICY,
		PILOT_CERT_PROVIDER,
		CA_ADDR,
		CA_SNI,
		POD_NAME,
		POD_NAMESPACE,
		IDENTITY_IP,
		INSTANCE_IP,
		SERVICE_ACCOUNT,
		HOST_IP,
		CANONICAL_SERVICE,
		CANONICAL_REVISION,
		PROXY_CONFIG,
		PILOT_SNI,
		ISTIO_META_CLUSTER_ID,
		ISTIO_META_INTERCEPTION_MODE,
		ISTIO_META_NETWORK,
		ISTIO_METAJSON_LABELS,
		ISTIO_METAJSON_ISTIO_ANNOTATIONS,
		ISTIO_META_WORKLOAD_NAME,
		ISTIO_META_OWNER,
		ISTIO_META_MESH_ID,
	}
)

func (d *SidecarData) IsIstioIngressGatewayHasIP() bool {
	return net.ParseIP(d.IstioIngressGatewayAddress) != nil
}

func (d *SidecarData) ForWorkload(workload *networkingCrd.WorkloadEntry) (*SidecarData, error) {
	proxyConfig := proto.Clone(d.IstioMeshConfig.GetDefaultConfig()).(*meshconfig.ProxyConfig)
	// set reasonable defaults
	proxyConfig.ServiceCluster = getServiceCluster(workload)
	proxyConfig.Concurrency = nil // by default, use all CPU cores of the VM
	// apply defaults for all mesh expansion proxies
	if value := d.ExpansionProxyConfig; value != "" {
		if err := gogoprotomarshal.ApplyYAML(value, proxyConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ProxyConfig for mesh expansion proxies [%v]: %w", value, err)
		}
	}

	// if address of the Istio Ingress Gateway is a DNS name rather than IP,
	// we cannot use /etc/hosts to remap *.svc.cluster.local DNS names
	if !d.IsIstioIngressGatewayHasIP() {
		replaceClusterLocalAddresses(proxyConfig, workload.Namespace, d.IstioIngressGatewayAddress)
	}

	// apply explicit configuration for that particular proxy
	if value := workload.Annotations[annotation.ProxyConfig.Name]; value != "" {
		if err := gogoprotomarshal.ApplyYAML(value, proxyConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ProxyConfig from %q annotation [%v]: %w", annotation.ProxyConfig.Name, value, err)
		}
	}

	d.Workload = workload
	d.ProxyConfig = proxyConfig

	return d, nil
}

func replaceClusterLocalAddresses(proxyConfig *meshconfig.ProxyConfig, workloadNamespace string, externalDnsName string) {
	replace := func(address string, addressSetter func(string), tlsSettings *networking.ClientTLSSettings) {
		if address == "" {
			return
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			host = address
		}
		if net.ParseIP(host) != nil {
			return // skip IP address
		}
		if !isClusterLocal(host) {
			return // skip non- cluster local address
		}
		externalAddress := externalDnsName
		if port != "" {
			externalAddress = net.JoinHostPort(externalDnsName, port)
		}
		addressSetter(externalAddress)
		if tlsSettings != nil && tlsSettings.GetMode() != networking.ClientTLSSettings_DISABLE {
			canonicalHost := canonicalClusterLocalAlias(host, workloadNamespace)
			shortHost := shortClusterLocalAlias(host, workloadNamespace)
			if tlsSettings.GetSni() == "" {
				tlsSettings.Sni = shortHost
			}
			for _, extraName := range []string {canonicalHost, shortHost} {
				registered := false
				for _, knownName := range tlsSettings.GetSubjectAltNames() {
					if extraName == knownName {
						registered = true
						break
					}
				}
				if !registered {
					tlsSettings.SubjectAltNames = append(tlsSettings.SubjectAltNames, extraName)
				}
			}
		}
	}

	replace(
		proxyConfig.GetDiscoveryAddress(),
		func(value string) {
			proxyConfig.DiscoveryAddress = value
		},
		nil,
	)

	replace(
		proxyConfig.GetTracing().GetZipkin().GetAddress(),
		func(value string) {
			proxyConfig.GetTracing().GetZipkin().Address = value
		},
		proxyConfig.GetTracing().GetTlsSettings(),
	)

	replace(
		proxyConfig.GetTracing().GetLightstep().GetAddress(),
		func(value string) {
			proxyConfig.GetTracing().GetLightstep().Address = value
		},
		proxyConfig.GetTracing().GetTlsSettings(),
	)

	replace(
		proxyConfig.GetTracing().GetDatadog().GetAddress(),
		func(value string) {
			proxyConfig.GetTracing().GetDatadog().Address = value
		},
		proxyConfig.GetTracing().GetTlsSettings(),
	)

	replace(
		proxyConfig.GetEnvoyAccessLogService().GetAddress(),
		func(value string) {
			proxyConfig.GetEnvoyAccessLogService().Address = value
		},
		proxyConfig.GetEnvoyAccessLogService().GetTlsSettings(),
	)

	replace(
		proxyConfig.GetEnvoyMetricsService().GetAddress(),
		func(value string) {
			proxyConfig.GetEnvoyMetricsService().Address = value
		},
		proxyConfig.GetEnvoyMetricsService().GetTlsSettings(),
	)

	// deprecated
	replace(
		proxyConfig.GetZipkinAddress(),
		func(value string) {
			proxyConfig.ZipkinAddress = value
		},
		proxyConfig.GetEnvoyMetricsService().GetTlsSettings(),
	)
}

func (d *SidecarData) GetCaAddr() string {
	if value := d.IstioConfigValues.GetGlobal().GetCaAddress(); value != "" {
		return value
	}
	return d.ProxyConfig.GetDiscoveryAddress()
}

func (d *SidecarData) GetEnv() ([]string, error) {
	vars := make([]string, 0, len(d.ProxyConfig.GetProxyMetadata())+len(SIDECAR_ENV))
	// lower priority
	for name, value := range d.ProxyConfig.GetProxyMetadata() {
		vars = append(vars, fmt.Sprintf("%s=%s", name, value))
	}
	// higher priority
	for _, envar := range SIDECAR_ENV {
		value, err := envar.Value(d)
		if err != nil {
			return nil, fmt.Errorf("failed to generate value of the environment variable %q: %w", envar.Name, err)
		}
		vars = append(vars, fmt.Sprintf("%s=%s", envar.Name, value))
	}
	return vars, nil
}

func (d *SidecarData) GetEnvFile() ([]byte, error) {
	vars, err := d.GetEnv()
	if err != nil {
		return nil, err
	}
	return []byte(strings.Join(vars, "\n")), nil
}

func (d *SidecarData) GetIstioProxyArgs() []string {
	return []string{
		"proxy",
		"sidecar",
		"--serviceCluster", // `istio-agent` will only respect this setting from command-line
		d.ProxyConfig.GetServiceCluster(),
		"--concurrency",
		fmt.Sprintf("%d", d.ProxyConfig.GetConcurrency().GetValue()), // `istio-agent` will only respect this setting from command-line
		"--proxyLogLevel",
		d.GetLogLevel(),
		"--proxyComponentLogLevel",
		d.GetComponentLogLevel(),
		"--trust-domain",
		d.GetTrustDomain(),
	}
}

func (d *SidecarData) GetIstioSystemNamespace() string {
	return d.IstioSystemNamespace
}

func (d *SidecarData) GetCanonicalDiscoveryAddress() string {
	revision := d.IstioConfigValues.GetGlobal().GetRevision()
	if revision != "" {
		revision = "-" + revision
	}
	return fmt.Sprintf("istiod%s.%s.svc:15012", revision, d.GetIstioSystemNamespace())
}

func (d *SidecarData) GetIstioProxyHosts() []string {
	if !d.IsIstioIngressGatewayHasIP() {
		// if address of the Istio Ingress Gateway is a DNS name rather than IP,
		// we cannot use /etc/hosts to remap *.svc.cluster.local DNS names
		return nil
	}
	candidates := []string{
		d.GetCanonicalDiscoveryAddress(),
		d.ProxyConfig.GetDiscoveryAddress(),
		d.ProxyConfig.GetTracing().GetZipkin().GetAddress(),
		d.ProxyConfig.GetTracing().GetLightstep().GetAddress(),
		d.ProxyConfig.GetTracing().GetDatadog().GetAddress(),
		d.ProxyConfig.GetTracing().GetTlsSettings().GetSni(),
		d.ProxyConfig.GetEnvoyAccessLogService().GetAddress(),
		d.ProxyConfig.GetEnvoyAccessLogService().GetTlsSettings().GetSni(),
		d.ProxyConfig.GetEnvoyMetricsService().GetAddress(),
		d.ProxyConfig.GetEnvoyMetricsService().GetTlsSettings().GetSni(),
		d.ProxyConfig.GetZipkinAddress(), // deprecated
		d.IstioConfigValues.GetGlobal().GetRemotePolicyAddress(),
		d.IstioConfigValues.GetGlobal().GetRemotePilotAddress(),
		d.IstioConfigValues.GetGlobal().GetRemoteTelemetryAddress(),
		d.IstioConfigValues.GetGlobal().GetCaAddress(),
	}
	hosts := make([]string, 0, len(candidates)*4)
	for _, candidate := range candidates {
		if candidate == "" {
			continue // skip undefined addresses
		}
		host, _, err := net.SplitHostPort(candidate)
		if err != nil {
			host = candidate
		}
		if net.ParseIP(host) != nil {
			continue // skip IP address
		}
		if !isClusterLocal(host) {
			continue // skip non- cluster local address
		}
		svc, ns := splitServiceAndNamespace(host, d.Workload.Namespace)
		hosts = append(hosts, getClusterLocalAliases(svc, ns)...)
	}
	return hosts
}

func (d *SidecarData) GetIstioProxyContainerName() string {
	return fmt.Sprintf("%s-%s-istio-proxy", d.Workload.Namespace, d.Workload.Name)
}

func (d *SidecarData) GetIstioProxyImage() string {
	if value := d.Workload.Annotations[annotation.SidecarProxyImage.Name]; value != "" {
		return value
	}
	hub := d.IstioConfigValues.GetGlobal().GetHub()
	if value := d.Workload.Annotations[bootstrapAnnotation.ProxyImageHub]; value != "" {
		hub = value
	}
	return fmt.Sprintf("%s/%s:%s",
		strings.TrimRight(hub, "/"),
		d.IstioConfigValues.GetGlobal().GetProxy().GetImage(),
		d.IstioConfigValues.GetGlobal().GetTag())
}

func getAppOrServiceAccount(workload *networkingCrd.WorkloadEntry) string {
	if value := workload.Spec.Labels["app"]; value != "" {
		return value
	}
	if value := workload.Labels["app"]; value != "" {
		return value
	}
	return workload.Spec.ServiceAccount
}

func getServiceCluster(workload *networkingCrd.WorkloadEntry) string {
	return fmt.Sprintf("%s.%s", getAppOrServiceAccount(workload), workload.Namespace)
}

func (d *SidecarData) GetTrustDomain() string {
	return d.IstioConfigValues.GetGlobal().GetTrustDomain()
}

func (d *SidecarData) GetLogLevel() string {
	if value := d.Workload.Annotations[annotation.SidecarLogLevel.Name]; value != "" {
		return value
	}
	if value := d.IstioConfigValues.GetGlobal().GetProxy().GetLogLevel(); value != "" {
		return value
	}
	return "info"
}

func (d *SidecarData) GetComponentLogLevel() string {
	if value := d.Workload.Annotations[annotation.SidecarComponentLogLevel.Name]; value != "" {
		return value
	}
	if value := d.IstioConfigValues.GetGlobal().GetProxy().GetComponentLogLevel(); value != "" {
		return value
	}
	return "misc:info"
}

func isClusterLocal(host string) bool {
	segments := strings.Split(host, ".")
	switch len(segments) {
	case 1, 2:
		return true // TODO(yskopets): beware of false positives like `docker.io`
	case 3:
		return segments[2] == "svc"
	case 4:
		return segments[2] == "svc" && segments[3] == "cluster"
	case 5:
		return segments[2] == "svc" && segments[3] == "cluster" && segments[4] == "local"
	default:
		return false
	}
}

func splitServiceAndNamespace(host, workloadNamespace string) (string, string) {
	segments := strings.SplitN(host, ".", 3)
	if len(segments) > 1 {
		return segments[0], segments[1]
	}
	return segments[0], workloadNamespace
}

func getClusterLocalAliases(svc, ns string) []string {
	base := svc + "." + ns
	return []string{
		base,
		base + ".svc",
		base + ".svc.cluster",
		base + ".svc.cluster.local",
	}
}

func shortClusterLocalAlias(host, workloadNamespace string) string {
	svc, ns := splitServiceAndNamespace(host, workloadNamespace)
	return fmt.Sprintf("%s.%s.svc", svc, ns)
}

func canonicalClusterLocalAlias(host, workloadNamespace string) string {
	svc, ns := splitServiceAndNamespace(host, workloadNamespace)
	return fmt.Sprintf("%s.%s.svc.cluster.local", svc, ns)
}

func clusterLocalSni(address, defaultSni string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if !isClusterLocal(host) {
		return defaultSni
	}
	return "" // use the default value
}

func addressToPodNameAddition(address string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(address)))[0:7]
}
