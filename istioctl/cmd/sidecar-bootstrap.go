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

package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	bootstrapBundle "istio.io/istio/istioctl/pkg/bootstrap/bundle"
	bootstrapSsh "istio.io/istio/istioctl/pkg/bootstrap/ssh"
	bootstrapSshFake "istio.io/istio/istioctl/pkg/bootstrap/ssh/fake"
	bootstrapUtil "istio.io/istio/istioctl/pkg/bootstrap/util"
	istioconfig "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type BootstrapBundle = bootstrapBundle.BootstrapBundle
type SidecarData = bootstrapBundle.SidecarData

const (
	// IP address or DNS name of the machine represented by this WorkloadEntry to use
	// instead of WorkloadEntry.Address for SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// This setting is intended for those scenarios where `istioctl x sidecar-bootstrap`
	// will be run on a machine without direct connectivity to the WorkloadEntry.Address.
	// E.g., one might set WorkloadEntry.Address to the "internal IP" of a VM
	// and set value of this annotation to the "external IP" of that VM.
	//
	// By default, value of WorkloadEntry.Address is assumed.
	sidecarBootstrapSshHostAnnotation = "sidecar-bootstrap.istioctl.istio.io/ssh-host"

	// Port of the SSH server on the machine represented by this WorkloadEntry to use
	// for SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// By default, `22` is assumed.
	sidecarBootstrapSshPortAnnotation = "sidecar-bootstrap.istioctl.istio.io/ssh-port"

	// User on the machine represented by this WorkloadEntry to use for SSH connections
	// from `istioctl x sidecar-bootstrap`.
	//
	// Make sure that user has enough permissions to create the config dir and
	// to run Docker container without `sudo`.
	//
	// By default, a user running `istioctl x sidecar-bootstrap` is assumed.
	sidecarBootstrapSshUserAnnotation = "sidecar-bootstrap.istioctl.istio.io/ssh-user"

	// Path to the `scp` binary on the machine represented by this WorkloadEntry to use
	// in SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// By default, `/usr/bin/scp` is assumed.
	sidecarBootstrapScpPathAnnotation = "sidecar-bootstrap.istioctl.istio.io/scp-path"

	// Directory on the machine represented by this WorkloadEntry where `istioctl x sidecar-bootstrap`
	// should copy bootstrap bundle to.
	//
	// By default, `/tmp/istio-proxy` is assumed (the most reliable default value for out-of-the-box experience).
	sidecarBootstrapDestinationDirAnnotation = "sidecar-bootstrap.istioctl.istio.io/destination-dir"
)

const (
	defaultDestinationDir = "/tmp/istio-proxy" // the most reliable default value for out-of-the-box experience
)

var (
	dryRun            bool
	all               bool
	tokenDuration     time.Duration
	outputDir         string
	defaultSshPort    int
	defaultSshUser    string
	sshConnectTimeout time.Duration
	sshAuthMethod     ssh.AuthMethod
	sshKeyLocation    string
	sshIgnoreHostKeys bool
	defaultScpOpts    = bootstrapSsh.CopyOpts{
		RemoteScpPath: "/usr/bin/scp",
	}
	startIstioProxy bool
)

var (
	sshConfig ssh.ClientConfig
)

type workloadIdentity struct {
	ServiceAccountToken []byte
}

var (
	sshClientFactory = newSshClient
)

func newSshClient(stdout, stderr io.Writer) bootstrapSsh.Client {
	if dryRun {
		return bootstrapSshFake.NewClient(stdout, stderr)
	} else {
		return bootstrapSsh.NewClient(stdout, stderr)
	}
}

func getConfigValuesFromConfigMap(kubeconfig string) (*istioconfig.Values, error) {
	valuesConfig, err := getValuesFromConfigMap(kubeconfig)
	if err != nil {
		return nil, err
	}
	values := new(istioconfig.Values)
	err = (&jsonpb.Unmarshaler{AllowUnknownFields: true}).Unmarshal(strings.NewReader(valuesConfig), values)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal Istio config values: %w", err)
	}
	return values, nil
}

func fetchSingleWorkloadEntry(client istioclient.Interface, workloadName string) ([]networking.WorkloadEntry, string, error) {
	workloadSplit := strings.Split(workloadName, ".")
	if len(workloadSplit) != 2 {
		return nil, "", fmt.Errorf("workload name %q is not in the format: workloadName.workloadNamespace", workloadName)
	}

	we, err := client.NetworkingV1alpha3().WorkloadEntries(workloadSplit[1]).Get(context.Background(), workloadSplit[0], metav1.GetOptions{})
	if we == nil || err != nil {
		return nil, "", fmt.Errorf("WorkloadEntry \"/namespaces/%s/workloadentries/%s\" was not found", workloadSplit[1], workloadSplit[0])
	}

	return []networking.WorkloadEntry{*we}, workloadSplit[1], nil
}

func fetchAllWorkloadEntries(client istioclient.Interface) ([]networking.WorkloadEntry, string, error) {
	list, err := client.NetworkingV1alpha3().WorkloadEntries(namespace).List(context.Background(), metav1.ListOptions{})
	return list.Items, namespace, err
}

func getK8sCaCert(kubeClient kubernetes.Interface) ([]byte, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "extension-apiserver-authentication", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap /namespaces/%s/configmaps/%s: %w", "kube-system", "extension-apiserver-authentication", err)
	}
	caCert := cm.Data["client-ca-file"]
	if caCert == "" {
		return nil, fmt.Errorf("expected ConfigMap /namespaces/%s/configmaps/%s to have a key %q", cm.Namespace, cm.Name, "client-ca-file")
	}
	return []byte(caCert), nil
}

func getIstioCaCert(kubeClient kubernetes.Interface, namespace string) ([]byte, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), "istio-ca-root-cert", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap /namespaces/%s/configmaps/%s: %w", namespace, "istio-ca-root-cert", err)
	}
	caCert := cm.Data[constants.CACertNamespaceConfigMapDataName]
	if caCert == "" {
		return nil, fmt.Errorf("expected ConfigMap /namespaces/%s/configmaps/%s to have a key %q", cm.Namespace, cm.Name, constants.CACertNamespaceConfigMapDataName)
	}
	return []byte(caCert), nil
}

func getIstioIngressGatewayService(kubeClient kubernetes.Interface, namespace, service string) (*corev1.Service, error) {
	svc, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Service /namespaces/%s/services/%s: %w", namespace, service, err)
	}
	return svc, nil
}

func verifyMeshExpansionPorts(svc *corev1.Service) error {
	ports := make(map[string]int32)
	for _, port := range svc.Spec.Ports {
		ports[port.Name] = port.Port
	}
	meshExpansionPorts := []struct {
		name string
		port int32
	}{
		{name: "tcp-istiod", port: 15012},
		{name: "tls", port: 15443},
	}
	for _, expected := range meshExpansionPorts {
		if actual, present := ports[expected.name]; !present || actual != expected.port {
			return fmt.Errorf("mesh expansion is not possible because Istio Ingress Gateway Service /namespaces/%s/services/%s is missing a port '%s (%d)'", svc.Namespace, svc.Name, expected.name, expected.port)
		}
	}
	return nil
}

func getIstioIngressGatewayAddress(svc *corev1.Service) (string, error) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return "", fmt.Errorf("Service /namespaces/%s/services/%s has no ingress points", svc.Namespace, svc.Name)
	}
	// prefer ingress point with IP
	for _, endpoint := range svc.Status.LoadBalancer.Ingress {
		if value := endpoint.IP; value != "" {
			return value, nil
		}
	}
	// fallback to ingress point with Hostname
	for _, endpoint := range svc.Status.LoadBalancer.Ingress {
		if value := endpoint.Hostname; value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("Service /namespaces/%s/services/%s has no valid ingress points", svc.Namespace, svc.Name)
}

func getIdentityForEachWorkload(
	kubeClient kubernetes.Interface,
	workloadEntries []networking.WorkloadEntry,
	namespace string) (map[string]workloadIdentity, error) {
	seenServiceAccounts := make(map[string]workloadIdentity)

	for _, entryCfg := range workloadEntries {
		wle := entryCfg.Spec
		if _, ok := seenServiceAccounts[wle.ServiceAccount]; ok {
			continue // only generate one token per ServiceAccount
		}
		if wle.ServiceAccount == "" {
			return nil, fmt.Errorf("cannot generate a ServiceAccount token for a WorkloadEntry \"/namespaces/%s/workloadentries/%s\" because ServiceAccount field is empty", entryCfg.Namespace, entryCfg.Name)
		}

		expirationSeconds := int64(tokenDuration / time.Second)
		resp, err := kubeClient.CoreV1().ServiceAccounts(entryCfg.Namespace).CreateToken(context.TODO(), wle.ServiceAccount,
			&authenticationv1.TokenRequest{
				Spec: authenticationv1.TokenRequestSpec{
					Audiences:         []string{"istio-ca"},
					ExpirationSeconds: &expirationSeconds,
				},
			}, metav1.CreateOptions{})

		if err != nil {
			return nil, fmt.Errorf("failed to generate a ServiceAccount token for a WorkloadEntry /namespaces/%s/workloadentries/%s: %w", entryCfg.Namespace, entryCfg.Name, err)
		}

		seenServiceAccounts[wle.ServiceAccount] = workloadIdentity{
			ServiceAccountToken: []byte(resp.Status.Token),
		}
	}
	return seenServiceAccounts, nil
}

func processWorkloads(kubeClient kubernetes.Interface,
	workloads []networking.WorkloadEntry,
	workloadIdentityMapping map[string]workloadIdentity,
	data *SidecarData,
	handle func(bundle BootstrapBundle) error) error {

	for _, workload := range workloads {
		identity, hasIdentity := workloadIdentityMapping[workload.Spec.ServiceAccount]
		if !hasIdentity {
			log.Warnf("skipping WorkloadEntry without a ServiceAccount: /namespaces/%s/workloadentries/%s", workload.Namespace, workload.Name)
			continue
		}

		data.Workload = &workload
		data.ProxyConfig = proto.Clone(data.IstioMeshConfig.GetDefaultConfig()).(*meshconfig.ProxyConfig)
		data.ProxyConfig.ServiceCluster = workload.Spec.ServiceAccount
		data.ProxyConfig.Concurrency = nil // by default, use all CPU cores of the VM
		if value := workload.Annotations[annotation.ProxyConfig.Name]; value != "" {
			if err := gogoprotomarshal.ApplyYAML(value, data.ProxyConfig); err != nil {
				return fmt.Errorf("failed to unmarshal ProxyConfig from %q annotation [%v]: %w", annotation.ProxyConfig.Name, value, err)
			}
		}

		environment, err := data.GetEnvFile()
		if err != nil {
			return err
		}

		bundle := BootstrapBundle{
			/* k8s */
			K8sCaCert: data.K8sCaCert,
			/* mesh */
			IstioCaCert:                data.IstioCaCert,
			IstioIngressGatewayAddress: data.IstioIngressGatewayAddress,
			/* workload */
			Workload:            workload,
			ServiceAccountToken: identity.ServiceAccountToken,
			/* sidecar */
			IstioProxyContainerName: data.GetIstioProxyContainerName(),
			IstioProxyImage:         data.GetIstioProxyImage(),
			IstioProxyEnvironment:   environment,
			IstioProxyArgs:          data.GetIstioProxyArgs(),
			IstioProxyHosts:         data.GetIstioProxyHosts(),
		}
		err = handle(bundle)
		if err != nil {
			return err
		}
	}
	return nil
}

func dumpBootstrapBundle(outputDir string, bundle BootstrapBundle) error {
	dump := func(filepath string, content []byte) error {
		err := ioutil.WriteFile(filepath, content, 0644)
		if err != nil {
			return fmt.Errorf("failed to dump into a file %q: %w", filepath, err)
		}
		return nil
	}
	if err := dump(path.Join(outputDir, "sidecar.env"), bundle.IstioProxyEnvironment); err != nil {
		return err
	}
	if err := dump(path.Join(outputDir, "k8s-ca.pem"), bundle.K8sCaCert); err != nil {
		return err
	}
	if err := dump(path.Join(outputDir, "istio-ca.pem"), bundle.IstioCaCert); err != nil {
		return err
	}
	if err := dump(path.Join(outputDir, "istio-token"), bundle.ServiceAccountToken); err != nil {
		return err
	}
	return nil
}

func copyBootstrapBundle(client bootstrapSsh.Client, bundle BootstrapBundle) error {
	host := bundle.Workload.Spec.Address
	if value := bundle.Workload.Annotations[sidecarBootstrapSshHostAnnotation]; value != "" {
		host = value
	}
	port := strconv.Itoa(defaultSshPort)
	if value := bundle.Workload.Annotations[sidecarBootstrapSshPortAnnotation]; value != "" {
		port = value
	}
	username := defaultSshUser
	if value := bundle.Workload.Annotations[sidecarBootstrapSshUserAnnotation]; value != "" {
		username = value
	}
	address := net.JoinHostPort(host, port)

	err := client.Dial(address, username, sshConfig)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir := defaultDestinationDir
	if value := bundle.Workload.Annotations[sidecarBootstrapDestinationDirAnnotation]; value != "" {
		remoteDir = value
	}

	// Ensure the remote directory exists.
	err = client.Exec("mkdir -p " + remoteDir)
	if err != nil {
		return err
	}

	scpOpts := defaultScpOpts
	if value := bundle.Workload.Annotations[sidecarBootstrapScpPathAnnotation]; value != "" {
		scpOpts.RemoteScpPath = value
	}

	remoteEnvPath := path.Join(remoteDir, "sidecar.env")
	err = client.Copy(bundle.IstioProxyEnvironment, remoteEnvPath, scpOpts)
	if err != nil {
		return err
	}

	remoteK8sCaPath := path.Join(remoteDir, "k8s-ca.pem")
	err = client.Copy(bundle.K8sCaCert, remoteK8sCaPath, scpOpts)
	if err != nil {
		return err
	}

	remoteIstioCaPath := path.Join(remoteDir, "istio-ca.pem")
	err = client.Copy(bundle.IstioCaCert, remoteIstioCaPath, scpOpts)
	if err != nil {
		return err
	}

	remoteIstioTokenPath := path.Join(remoteDir, "istio-token")
	err = client.Copy(bundle.ServiceAccountToken, remoteIstioTokenPath, scpOpts)
	if err != nil {
		return err
	}

	cmd := []string{
		"docker",
		"run",
		"-d",
		"--name",
		bundle.IstioProxyContainerName,
		"--restart",
		"unless-stopped",
		"--network",
		"host", // you need to deal with Sidecar CR if you want it to be "non-captured" mode
		"-v",
		remoteIstioCaPath + ":" + "/var/run/secrets/istio/root-cert.pem", // "./var/run/secrets/istio/root-cert.pem" is a hardcoded value in `istio-agent` that corresponds to `PILOT_CERT_PROVIDER == istiod`
		"-v",
		remoteIstioTokenPath + ":" + "/var/run/secrets/tokens/istio-token", // "./var/run/secrets/tokens/istio-token" is a hardcoded value in `istio-agent` that corresponds to `JWT_POLICY == third-party-jwt`
		"-v",
		remoteK8sCaPath + ":" + "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", // "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" is a well-known k8s path heavily abused in k8s world
		"--env-file",
		remoteEnvPath,
	}
	for _, host := range bundle.IstioProxyHosts {
		cmd = append(cmd,
			"--add-host",
			host+":"+bundle.IstioIngressGatewayAddress,
		)
	}
	cmd = append(cmd, bundle.IstioProxyImage)
	cmd = append(cmd, bundle.IstioProxyArgs...)

	if startIstioProxy {
		if err := client.Exec(fmt.Sprintf("docker rm --force %s", bundle.IstioProxyContainerName)); err != nil {
			log.Warna(err)
		}

		if err := client.Exec(strings.Join(cmd, " ")); err != nil {
			return err
		}
	}
	return nil
}

func deriveSSHMethod(in io.Reader) (errs error) {
	call := func(fn func() error) {
		if fn == nil {
			return
		}
		err := fn()
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if sshKeyLocation == "" {
		rawModeStdin, restoreStdin, err := bootstrapUtil.RawModeStdin(in)
		if err != nil {
			return err
		}
		defer call(restoreStdin)
		term := terminal.NewTerminal(rawModeStdin, "")
		sshPassword, err := term.ReadPassword("Please enter the SSH password: ")
		if err != nil {
			return err
		}
		if sshPassword == "" {
			return fmt.Errorf("a password, or SSH key location is required for sidecar-bootstrap")
		}
		sshAuthMethod = ssh.Password(sshPassword)
	} else {
		// Attempt to parse the key.
		rawKey, err := ioutil.ReadFile(sshKeyLocation)
		if err != nil {
			return fmt.Errorf("failed to read SSH key from %q: %w", sshKeyLocation, err)
		}
		key, err := ssh.ParsePrivateKey(rawKey)
		if err != nil {
			if err, ok := err.(*ssh.PassphraseMissingError); ok {
				rawModeStdin, restoreStdin, err := bootstrapUtil.RawModeStdin(in)
				if err != nil {
					return err
				}
				defer call(restoreStdin)
				term := terminal.NewTerminal(rawModeStdin, "")
				sshKeyPassword, err := term.ReadPassword("Please enter the password for the SSH key: ")
				if err != nil {
					return err
				}
				decryptedKey, err := ssh.ParsePrivateKeyWithPassphrase(rawKey, []byte(sshKeyPassword))
				if err != nil {
					return fmt.Errorf("failed to parse password-protected SSH key from %q: %w", sshKeyLocation, err)
				}
				sshAuthMethod = ssh.PublicKeys(decryptedKey)
			} else {
				return fmt.Errorf("failed to parse SSH key from %q: %w", sshKeyLocation, err)
			}
		} else {
			sshAuthMethod = ssh.PublicKeys(key)
		}
	}
	return nil
}

func vmBootstrapCommand() *cobra.Command {
	vmBSCommand := &cobra.Command{
		Use:   "sidecar-bootstrap <workloadEntry>.<namespace>",
		Short: "(experimental) bootstraps a non-kubernetes workload (e.g. VM, Baremetal) onto an Istio mesh",
		Long: `(experimental) Takes in one or more WorkloadEntries, generates identities for them, and copies to
the particular identities to the workloads over SSH. Optionally allowing for saving the identities locally
for use in CI like environments, and starting istio-proxy where no special configuration is needed.
This allows for workloads to participate in the Istio mesh.

To autenticate to a remote node you can use either SSH Keys, or SSH Passwords. If using passwords you
must have a TTY for you to be asked your password, we do not accept an argument for it so it
cannot be left inside your shell history.

Copying is performed with scp, and as such is required if you'd like to copy a file over.
If SCP is not at the standard path "/usr/bin/scp", you should provide it's location with
the "--remote-scp-path" option.

In order to start Istio on the remote node you must have docker installed on the remote node.
Istio will be started on the host network as a docker container in capture mode.`,
		Example: `  # Show planned actions to copy workload identity for a VM represented by the WorkloadEntry named "we" in the "ns" namespace:
  istioctl x sidecar-bootstrap we.ns --dry-run

  # Show planned actions to copy workload identity and start Istio proxy in a VM represented by the WorkloadEntry named "we" in the "ns" namespace:
  istioctl x sidecar-bootstrap we.ns --start-istio-proxy --dry-run

  # Copy workload identity into a VM represented by the WorkloadEntry named "we" in the "ns" namespace:
  istioctl x sidecar-bootstrap we.ns

  # Copy workload identity and start Istio proxy in a VM represented by the WorkloadEntry named "we" in the "ns" namespace:
  istioctl x sidecar-bootstrap we.ns --start-istio-proxy

  # Generate workload identity for a VM represented by the WorkloadEntry named "we" in the "ns" namespace; and save generated files into a local directory:
  istioctl x sidecar-bootstrap we.ns --local-dir path/where/i/want/workload/identity`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !all {
				return fmt.Errorf("sidecar-bootstrap requires either a WorkloadEntry or the --all flag")
			}
			if len(args) > 0 && all {
				return fmt.Errorf("sidecar-bootstrap requires either a WorkloadEntry or the --all flag but not both")
			}
			if all && namespace == "" {
				return fmt.Errorf("sidecar-bootstrap needs a namespace if fetching all WorkloadEntry(s)")
			}
			if defaultSshUser == "" {
				user, err := user.Current()
				if err != nil {
					return fmt.Errorf("failed to determine current user: %w", err)
				}
				defaultSshUser = user.Username
			}
			if outputDir == "" && !dryRun {
				err := deriveSSHMethod(cmd.InOrStdin())
				if err != nil {
					return err
				}
			}
			if !dryRun {
				var callback ssh.HostKeyCallback
				if sshIgnoreHostKeys {
					callback = ssh.InsecureIgnoreHostKey()
				} else {
					user, err := user.Current()
					if err != nil {
						return fmt.Errorf("failed to determine current user: %w", err)
					}
					knownhost, err := knownhosts.New(filepath.Join(user.HomeDir, ".ssh", "known_hosts"))
					if err != nil {
						return fmt.Errorf("failed to parse $HOME/.ssh/known_hosts: %w", err)
					}
					prompt := bootstrapSsh.HostKeyPrompt(cmd.InOrStdin(), cmd.ErrOrStderr())
					callback = bootstrapSsh.HostKeyCallbackChain(knownhost, prompt)
				}

				sshConfig = ssh.ClientConfig{
					User:            defaultSshUser,
					Auth:            []ssh.AuthMethod{sshAuthMethod},
					HostKeyCallback: callback,
					Timeout:         sshConnectTimeout,
				}
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := interfaceFactory(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %w", err)
			}

			configClient, err := configStoreFactory()
			if err != nil {
				return fmt.Errorf("failed to create Istio config client: %w", err)
			}

			var entries []networking.WorkloadEntry
			var chosenNS string
			if all {
				entries, chosenNS, err = fetchAllWorkloadEntries(configClient)
			} else {
				entries, chosenNS, err = fetchSingleWorkloadEntry(configClient, args[0])
			}
			if err != nil {
				return fmt.Errorf("unable to find WorkloadEntry(s): %w", err)
			}

			meshConfig, err := getMeshConfigFromConfigMap(kubeconfig, c.CommandPath())
			if err != nil {
				return fmt.Errorf("failed to read Istio Mesh configuration: %w", err)
			}

			istioConfigValues, err := getConfigValuesFromConfigMap(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to read Istio global values: %w", err)
			}

			if actual, expected := istioConfigValues.GetGlobal().GetJwtPolicy(), "third-party-jwt"; actual != expected {
				return fmt.Errorf("jwt policy is set to %q. At the moment, %q command only supports jwt policy %q", actual, c.CommandPath(), expected)
			}

			if actual, expected := istioConfigValues.GetGlobal().GetPilotCertProvider(), "istiod"; actual != expected {
				return fmt.Errorf("pilot cert provider is set to %q. At the moment, %q command only supports pilot cert provider %q", actual, c.CommandPath(), expected)
			}

			k8sCaCert, err := getK8sCaCert(kubeClient)
			if err != nil {
				return fmt.Errorf("unable to find k8s CA cert: %w", err)
			}

			istioCaCert, err := getIstioCaCert(kubeClient, istioNamespace)
			if err != nil {
				return fmt.Errorf("unable to find Istio CA cert: %w", err)
			}

			ingressServiceName := "istio-ingressgateway" // fallback value according to Istio docs
			if value := meshConfig.GetIngressService(); value != "" {
				ingressServiceName = value
			}
			ingressSvc, err := getIstioIngressGatewayService(kubeClient, istioNamespace, ingressServiceName)
			if err != nil {
				return fmt.Errorf("unable to find Istio Ingress Gateway: %w", err)
			}

			if err := verifyMeshExpansionPorts(ingressSvc); err != nil {
				return fmt.Errorf("Istio Ingress Gateway is not configured for mesh expansion: %w", err)
			}

			istioGatewayAddress, err := getIstioIngressGatewayAddress(ingressSvc)
			if err != nil {
				return fmt.Errorf("unable to find address of the Istio Ingress Gateway: %w", err)
			}

			if net.ParseIP(istioGatewayAddress) == nil {
				return fmt.Errorf("Istio Ingress Gateway uses a hostname %q rather than IP address. At the moment, %q command only supports Ingress Gateways with IP address", istioGatewayAddress, c.CommandPath())
			}

			identities, err := getIdentityForEachWorkload(kubeClient, entries, chosenNS)
			if err != nil {
				return fmt.Errorf("failed to generate security token(s) for WorkloadEntry(s): %w", err)
			}

			var action func(bundle BootstrapBundle) error
			if outputDir != "" {
				action = func(bundle BootstrapBundle) error {
					bundleDir := filepath.Join(outputDir, bundle.Workload.Namespace, bundle.Workload.Name)
					err = os.MkdirAll(bundleDir, os.ModePerm)
					if err != nil && !os.IsExist(err) {
						return fmt.Errorf("failed to create a local output directory %q: %w", bundleDir, err)
					}
					return dumpBootstrapBundle(bundleDir, bundle)
				}
			} else {
				action = func(bundle BootstrapBundle) error {
					sshClient := sshClientFactory(c.OutOrStdout(), c.ErrOrStderr())
					return copyBootstrapBundle(sshClient, bundle)
				}
			}

			data := &SidecarData{
				K8sCaCert:                  k8sCaCert,
				IstioSystemNamespace:       istioNamespace,
				IstioMeshConfig:            meshConfig,
				IstioConfigValues:          istioConfigValues,
				IstioCaCert:                istioCaCert,
				IstioIngressGatewayAddress: istioGatewayAddress,
			}
			return processWorkloads(kubeClient, entries, identities, data, action)
		},
	}

	vmBSCommand.PersistentFlags().BoolVarP(&all, "all", "a", false,
		"attempt to bootstrap all WorkloadEntry(s) in that namespace")
	vmBSCommand.PersistentFlags().DurationVar(&tokenDuration, "duration", 24*time.Hour,
		"(experimental) duration the generated ServiceAccount tokens are valid for.")
	vmBSCommand.PersistentFlags().StringVarP(&outputDir, "local-dir", "d", "",
		"directory to put bootstrap bundle(s) in locally as opposed to copying")
	vmBSCommand.PersistentFlags().DurationVar(&defaultScpOpts.Timeout, "timeout", 60*time.Second,
		"(experimental) the timeout for copying a bootstrap bundle")
	vmBSCommand.PersistentFlags().BoolVar(&sshIgnoreHostKeys, "ignore-host-keys", false,
		"(experimental) ignore host keys on the remote host")
	vmBSCommand.PersistentFlags().StringVarP(&sshKeyLocation, "ssh-key", "k", "",
		"(experimental) the location of the SSH key")
	vmBSCommand.PersistentFlags().IntVar(&defaultSshPort, "ssh-port", 22,
		"(experimental) default port to SSH to (is only effective unless the 'sidecar-bootstrap.istioctl.istio.io/ssh-port' annotation is present on a WorkloadEntry)")
	vmBSCommand.PersistentFlags().StringVarP(&defaultSshUser, "ssh-user", "u", "",
		"(experimental) default user to SSH as, defaults to the current user (is only effective unless the 'sidecar-bootstrap.istioctl.istio.io/ssh-user' annotation is present on a WorkloadEntry)")
	vmBSCommand.PersistentFlags().DurationVar(&sshConnectTimeout, "ssh-connect-timeout", 10*time.Second,
		"(experimental) the maximum amount of time to establish SSH connection")
	vmBSCommand.PersistentFlags().BoolVar(&startIstioProxy, "start-istio-proxy", false,
		"start Istio proxy on a remote host after copying workload identity")
	vmBSCommand.PersistentFlags().BoolVar(&dryRun, "dry-run", false,
		"show generated configuration and respective SSH commands but don't connect to, copy files or execute commands remotely")

	// same options as in `istioctl inject`
	vmBSCommand.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", defaultMeshConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", configMapKey))
	vmBSCommand.PersistentFlags().StringVar(&injectConfigMapName, "injectConfigMapName", defaultInjectConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio sidecar injection, key should be %q", injectConfigMapKey))

	return vmBSCommand
}
