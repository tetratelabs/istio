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
	"bufio"
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
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/gogo/protobuf/jsonpb"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	bootstrapAnnotation "istio.io/istio/istioctl/pkg/bootstrap/annotation"
	bootstrapBundle "istio.io/istio/istioctl/pkg/bootstrap/bundle"
	bootstrapSsh "istio.io/istio/istioctl/pkg/bootstrap/ssh"
	bootstrapSshFake "istio.io/istio/istioctl/pkg/bootstrap/ssh/fake"
	bootstrapUtil "istio.io/istio/istioctl/pkg/bootstrap/util"
	"istio.io/istio/istioctl/pkg/util/handlers"
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

var resourceURI = bootstrapUtil.ResourceURI

const (
	defaultProxyConfigDir = "/tmp/istio-proxy" // the most reliable default value for out-of-the-box experience
)

var (
	dryRun            bool
	all               bool
	tokenDuration     time.Duration
	outputDir         string
	defaultSSHPort    int
	defaultSSHUser    string
	sshConnectTimeout time.Duration
	sshAuthMethod     ssh.AuthMethod
	useSSHPassword    bool
	sshKeyLocation    string
	sshIgnoreHostKeys bool
	defaultScpOpts    = bootstrapSsh.CopyOpts{
		RemoteScpPath: "/usr/bin/scp",
	}
	startIstioProxy bool
	printDocs       bool
)

var (
	sshConfig ssh.ClientConfig
)

type workloadIdentity struct {
	ServiceAccountToken []byte
}

var (
	sshClientFactory = newSSHClient
)

func newSSHClient(stdout, stderr io.Writer) bootstrapSsh.Client {
	if dryRun {
		return bootstrapSshFake.NewClient(stdout, stderr)
	}
	return bootstrapSsh.NewClient(stdout, stderr)
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

func getExpansionProxyConfig(kubeClient kubernetes.Interface, namespace string) (string, error) {
	ns, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to read Namespace %q: %w", namespace, err)
	}
	configMapName := ns.Annotations[bootstrapAnnotation.MeshExpansionConfigMapName.Name]
	if configMapName == "" {
		return "", nil
	}
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), configMapName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to read ConfigMap %s referred to from the %q annotation on the Namespace "+
			"%q: %w", resourceURI("v1", "configmaps", namespace, configMapName), bootstrapAnnotation.MeshExpansionConfigMapName.Name, namespace, err)
	}
	value := cm.Data["PROXY_CONFIG"]
	if value == "" {
		return "", nil
	}
	proxyConfig := new(meshconfig.ProxyConfig)
	if err := gogoprotomarshal.ApplyYAML(value, proxyConfig); err != nil {
		return "", fmt.Errorf("failed to unmarshal ProxyConfig from the ConfigMap %s referred to from the %q "+
			"annotation on the Namespace %q: %w", resourceURI("v1", "configmaps", cm.Namespace, cm.Name),
			bootstrapAnnotation.MeshExpansionConfigMapName.Name, namespace, err)
	}
	return value, nil
}

func fetchSingleWorkloadEntry(client istioclient.Interface, namespace, workloadName string) ([]networking.WorkloadEntry, error) {
	we, err := client.NetworkingV1alpha3().WorkloadEntries(namespace).Get(context.Background(), workloadName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch WorkloadEntry %s: %w",
			resourceURI("networking.istio.io/v1beta1", "workloadentries", namespace, workloadName), err)
	}
	return []networking.WorkloadEntry{*we}, nil
}

func fetchAllWorkloadEntries(client istioclient.Interface, namespace string) ([]networking.WorkloadEntry, error) {
	list, err := client.NetworkingV1alpha3().WorkloadEntries(namespace).List(context.Background(), metav1.ListOptions{})
	return list.Items, err
}

func getK8sCaCertFromConfigMap(kubeClient kubernetes.Interface, namespace string) ([]byte, error) {
	ns, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read Namespace %q: %w", namespace, err)
	}
	configMapName := ns.Annotations[bootstrapAnnotation.K8sCaRootCertConfigMapName.Name]
	if configMapName == "" {
		return nil, fmt.Errorf("k8s Namespace %q has no a config map that would hold the root cert of a k8s CA", namespace)
	}
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read ConfigMap %s referred to from the %q annotation on the Namespace %q: %w",
			resourceURI("v1", "configmaps", namespace, configMapName), bootstrapAnnotation.MeshExpansionConfigMapName.Name, namespace, err)
	}
	value := cm.Data["ca.crt"] // well-known k8s constant
	if value == "" {
		return nil, fmt.Errorf("there is no root cert of a k8s CA in the ConfigMap %s", resourceURI("v1", "configmaps", cm.Namespace, cm.Name))
	}
	return []byte(value), nil
}

func getK8sCaCertFromServiceAccountTokenSecret(kubeClient kubernetes.Interface, namespace string) ([]byte, error) {
	sa, err := kubeClient.CoreV1().ServiceAccounts(namespace).Get(context.Background(), "default", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read ServiceAccount %s: %w", resourceURI("v1", "serviceaccounts", namespace, "default"), err)
	}
	for _, ref := range sa.Secrets {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(context.Background(), ref.Name, metav1.GetOptions{})
		if err != nil {
			log.Debugf("%v", err)
			continue
		}
		if secret.Type != "kubernetes.io/service-account-token" {
			continue
		}
		value := secret.Data["ca.crt"]
		if len(value) == 0 {
			continue
		}
		return value, nil
	}
	return nil, fmt.Errorf("unable to find a Secret with the root cert of a k8s CA among ServiceAccountToken Secrets of the ServiceAccount %s",
		resourceURI("v1", "serviceaccounts", sa.Namespace, sa.Name))
}

func getK8sCaCert(kubeClient kubernetes.Interface, namespace, istioNamespace string) ([]byte, error) {
	type K8sCaRootCertSource func(kubeClient kubernetes.Interface) ([]byte, error)
	sources := []K8sCaRootCertSource{
		func(kubeClient kubernetes.Interface) ([]byte, error) {
			return getK8sCaCertFromConfigMap(kubeClient, istioNamespace)
		},
		func(kubeClient kubernetes.Interface) ([]byte, error) {
			return getK8sCaCertFromServiceAccountTokenSecret(kubeClient, istioNamespace)
		},
		func(kubeClient kubernetes.Interface) ([]byte, error) {
			return getK8sCaCertFromServiceAccountTokenSecret(kubeClient, namespace)
		},
		func(kubeClient kubernetes.Interface) ([]byte, error) {
			return getK8sCaCertFromServiceAccountTokenSecret(kubeClient, "kube-public")
		},
	}
	for _, source := range sources {
		value, err := source(kubeClient)
		if err != nil {
			log.Debugf("%v", err)
			continue
		}
		return value, nil
	}
	return nil, fmt.Errorf("all supported strategies to find a k8s CA have failed.\n"+
		"To overcome this, either grant the user permissions to read k8s Secrets in one of the Namespaces %v,\n"+
		"or create a ConfigMap with the root cert of a k8s CA in the %q Namespace and use %q annotation to give this command a hint where to find such a ConfigMap",
		[]string{istioNamespace, namespace, "kube-public"}, istioNamespace, bootstrapAnnotation.K8sCaRootCertConfigMapName.Name)
}

func getIstioCaCert(kubeClient kubernetes.Interface, namespace string) ([]byte, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), "istio-ca-root-cert", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", resourceURI("v1", "configmaps", namespace, "istio-ca-root-cert"), err)
	}
	caCert := cm.Data[constants.CACertNamespaceConfigMapDataName]
	if caCert == "" {
		return nil, fmt.Errorf("expected ConfigMap %s to have a key %q", resourceURI("v1", "configmaps", cm.Namespace, cm.Name),
			constants.CACertNamespaceConfigMapDataName)
	}
	return []byte(caCert), nil
}

func getIstioIngressGatewayService(kubeClient kubernetes.Interface, namespace, service string) (*corev1.Service, error) {
	svc, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Service %s: %w", resourceURI("v1", "services", namespace, service), err)
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
			return fmt.Errorf("mesh expansion is not possible because Istio Ingress Gateway Service %s is missing a port '%s (%d)'",
				resourceURI("v1", "services", svc.Namespace, svc.Name), expected.name, expected.port)
		}
	}
	return nil
}

func getIstioIngressGatewayAddress(svc *corev1.Service) (string, error) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return "", fmt.Errorf("k8s Service %s has no ingress points", resourceURI("v1", "services", svc.Namespace, svc.Name))
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
	return "", fmt.Errorf("k8s Service %s has no valid ingress points", resourceURI("v1", "services", svc.Namespace, svc.Name))
}

func getIdentityForEachWorkload(
	kubeClient kubernetes.Interface,
	workloadEntries []networking.WorkloadEntry) (map[string]workloadIdentity, error) {
	seenServiceAccounts := make(map[string]workloadIdentity)

	for _, entryCfg := range workloadEntries {
		wle := entryCfg.Spec
		if _, ok := seenServiceAccounts[wle.ServiceAccount]; ok {
			continue // only generate one token per ServiceAccount
		}
		if wle.ServiceAccount == "" {
			return nil, fmt.Errorf("cannot generate a ServiceAccount token for a WorkloadEntry %s because ServiceAccount field is empty",
				resourceURI("networking.istio.io/v1beta1", "workloadentries", entryCfg.Namespace, entryCfg.Name))
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
			return nil, fmt.Errorf("failed to generate a ServiceAccount token for a WorkloadEntry %s: %w",
				resourceURI("networking.istio.io/v1beta1", "workloadentries", entryCfg.Namespace, entryCfg.Name), err)
		}

		seenServiceAccounts[wle.ServiceAccount] = workloadIdentity{
			ServiceAccountToken: []byte(resp.Status.Token),
		}
	}
	return seenServiceAccounts, nil
}

func processWorkloads(
	workloads []networking.WorkloadEntry,
	workloadIdentityMapping map[string]workloadIdentity,
	templateData *SidecarData,
	handle func(bundle BootstrapBundle) error) error {

	for _, workload := range workloads {
		identity, hasIdentity := workloadIdentityMapping[workload.Spec.ServiceAccount]
		if !hasIdentity {
			log.Warnf("skipping WorkloadEntry without a ServiceAccount: %s",
				resourceURI("networking.istio.io/v1beta1", "workloadentries", workload.Namespace, workload.Name))
			continue
		}

		data, err := templateData.ForWorkload(&workload)
		if err != nil {
			return err
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
	dump := func(filepath string, perm os.FileMode, content []byte) error {
		err := ioutil.WriteFile(filepath, content, perm)
		if err != nil {
			return fmt.Errorf("failed to dump into a file %q: %w", filepath, err)
		}
		return nil
	}
	configFilePerm := os.FileMode(0644)
	secretFilePerm := os.FileMode(0640)
	if err := dump(path.Join(outputDir, "sidecar.env"), configFilePerm, bundle.IstioProxyEnvironment); err != nil {
		return err
	}
	if err := dump(path.Join(outputDir, "k8s-ca.pem"), configFilePerm, bundle.K8sCaCert); err != nil {
		return err
	}
	if err := dump(path.Join(outputDir, "istio-ca.pem"), configFilePerm, bundle.IstioCaCert); err != nil {
		return err
	}
	if err := dump(path.Join(outputDir, "istio-token"), secretFilePerm, bundle.ServiceAccountToken); err != nil {
		return err
	}
	return nil
}

func copyBootstrapBundle(client bootstrapSsh.Client, bundle BootstrapBundle) error {
	host := bundle.Workload.Spec.Address
	if value := bundle.Workload.Annotations[bootstrapAnnotation.SSHHost.Name]; value != "" {
		host = value
	}
	port := strconv.Itoa(defaultSSHPort)
	if value := bundle.Workload.Annotations[bootstrapAnnotation.SSHPort.Name]; value != "" {
		port = value
	}
	username := defaultSSHUser
	if value := bundle.Workload.Annotations[bootstrapAnnotation.SSHUser.Name]; value != "" {
		username = value
	}
	address := net.JoinHostPort(host, port)

	err := client.Dial(address, username, sshConfig)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir := defaultProxyConfigDir
	if value := bundle.Workload.Annotations[bootstrapAnnotation.ProxyConfigDir.Name]; value != "" {
		remoteDir = value
	}

	// Ensure the remote directory exists.
	err = client.Exec("mkdir -p " + remoteDir)
	if err != nil {
		return err
	}

	scpOpts := defaultScpOpts
	if value := bundle.Workload.Annotations[bootstrapAnnotation.ScpPath.Name]; value != "" {
		scpOpts.RemoteScpPath = value
	}

	configFilePerm := os.FileMode(0644)
	secretFilePerm := os.FileMode(0640)

	remoteEnvPath := path.Join(remoteDir, "sidecar.env")
	err = client.Copy(bundle.IstioProxyEnvironment, remoteEnvPath, configFilePerm, scpOpts)
	if err != nil {
		return err
	}

	remoteK8sCaPath := path.Join(remoteDir, "k8s-ca.pem")
	err = client.Copy(bundle.K8sCaCert, remoteK8sCaPath, configFilePerm, scpOpts)
	if err != nil {
		return err
	}

	remoteIstioCaPath := path.Join(remoteDir, "istio-ca.pem")
	err = client.Copy(bundle.IstioCaCert, remoteIstioCaPath, configFilePerm, scpOpts)
	if err != nil {
		return err
	}

	remoteIstioTokenPath := path.Join(remoteDir, "istio-token")
	err = client.Copy(bundle.ServiceAccountToken, remoteIstioTokenPath, secretFilePerm, scpOpts)
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
		// "./var/run/secrets/istio/root-cert.pem" is a hardcoded value in `istio-agent` that corresponds to `PILOT_CERT_PROVIDER == istiod`
		remoteIstioCaPath + ":" + "/var/run/secrets/istio/root-cert.pem",
		"-v",
		// "./var/run/secrets/tokens/istio-token" is a hardcoded value in `istio-agent` that corresponds to `JWT_POLICY == third-party-jwt`
		remoteIstioTokenPath + ":" + "/var/run/secrets/tokens/istio-token",
		"-v",
		// "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" is a well-known k8s path heavily abused in k8s world
		remoteK8sCaPath + ":" + "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
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
	readSSHPassword := func() (secret string, errs error) {
		call := func(fn func() error) {
			if fn == nil {
				return
			}
			err := fn()
			if err != nil {
				errs = multierror.Append(errs, err)
			}
		}

		rawModeStdin, restoreStdin, err := bootstrapUtil.RawModeStdin(in)
		if err != nil {
			return "", err
		}
		defer call(restoreStdin)
		term := terminal.NewTerminal(rawModeStdin, "")
		sshPassword, err := term.ReadPassword("Please enter the SSH password: ")
		if err != nil {
			return "", err
		}
		if sshPassword == "" {
			return "", fmt.Errorf("SSH password cannot be empty")
		}
		return sshPassword, nil
	}
	parseSSHKey := func(rawKey []byte, name string) (_ ssh.Signer, errs error) {
		call := func(fn func() error) {
			if fn == nil {
				return
			}
			err := fn()
			if err != nil {
				errs = multierror.Append(errs, err)
			}
		}

		key, err := ssh.ParsePrivateKey(rawKey)
		if err == nil {
			return key, nil
		}
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			rawModeStdin, restoreStdin, err := bootstrapUtil.RawModeStdin(in)
			if err != nil {
				return nil, err
			}
			defer call(restoreStdin)
			term := terminal.NewTerminal(rawModeStdin, "")
			sshKeyPassword, err := term.ReadPassword(fmt.Sprintf("Please enter the password for the SSH key %q: ", name))
			if err != nil {
				return nil, err
			}
			decryptedKey, err := ssh.ParsePrivateKeyWithPassphrase(rawKey, []byte(sshKeyPassword))
			if err != nil {
				return nil, fmt.Errorf("failed to parse password-protected SSH key: %w", err)
			}
			return decryptedKey, nil
		}
		return nil, err
	}
	if useSSHPassword {
		sshPassword, err := readSSHPassword()
		if err != nil {
			return err
		}
		sshAuthMethod = ssh.Password(sshPassword)
		return nil
	}

	var candidateKeyLocations []string
	if sshKeyLocation != "" {
		candidateKeyLocations = []string{sshKeyLocation}
	} else {
		homeDir, err := homedir.Dir()
		if err != nil {
			return fmt.Errorf("failed to determine home directory of the current user: %w", err)
		}
		candidateKeyLocations = []string{
			filepath.Join(homeDir, ".ssh", "id_dsa"),
			filepath.Join(homeDir, ".ssh", "id_ecdsa"),
			filepath.Join(homeDir, ".ssh", "id_ed25519"),
			filepath.Join(homeDir, ".ssh", "id_rsa"),
		}
	}
	for _, candidateKeyLocation := range candidateKeyLocations {
		// Attempt to parse the key.
		rawKey, err := ioutil.ReadFile(candidateKeyLocation)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to read SSH key from %q: %w", candidateKeyLocation, err))
			continue
		}
		key, err := parseSSHKey(rawKey, candidateKeyLocation)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to parse SSH key from %q: %w", candidateKeyLocation, err))
			return // stop iterating over candidate keys after the first parse failure
		}
		sshAuthMethod = ssh.PublicKeys(key)
		return nil
	}
	return
}

type VmBootstrapCommandOpts struct {
	// ParentCommandDocPath is a full path of the parent command that should be used in help messages.
	//
	// By default, "istioctl x" is assumed.
	ParentCommandDocPath string
}

func NewVmBootstrapCommand(opts VmBootstrapCommandOpts) *cobra.Command {
	if opts.ParentCommandDocPath == "" {
		opts.ParentCommandDocPath = "istioctl x"
	}
	vmBSCommand := &cobra.Command{
		Use:   "sidecar-bootstrap [<workload-entry-name>[.<namespace>]]",
		Short: "(experimental) Bootstrap Istio Sidecar for a workload that runs on VM or Baremetal (mesh expansion scenarios)",
		Long: fmt.Sprintf(`(experimental) Takes in one or more WorkloadEntry(s), generates identity(s) for them,
and optionally copies generated files to the remote node(s) over SSH protocol and starts Istio Sidecar(s) there.

Alternatively, if SSH is not enabled on the remote node(s), generated files can be saved locally instead.
In that case you will be able to transfer files to the remote node(s) using a mechanism that suits best your particular environment.

If you choose to copy generated files to the remote node(s) over SSH, you will be required to provide SSH credentials,
i.e. either SSH Key or SSH Password.
If you want to use SSH Password or passphrase-protected SSH Key, you must run this command on an interactive terminal to type the password in.
We do not accept passwords through command line options to avoid leaking secrets into shell history.

File copying is performed over SCP protocol, and as such SCP binary must be installed on the remote node.
If SCP is installed in a location other than "/usr/bin/scp", you have to provide absolute path to the SCP binary
by adding %[1]q annotation to the respective WorkloadEntry resource.

To start Istio Sidecar on the remote node you must have Docker installed there.
Istio Sidecar will be started on the host network as a docker container in capture mode.

While this command can work without any explicit configuration, it is also possible to fine tune its behavior
by adding various annotations on a WorkloadEntry resource. E.g., consider the following real life example:

  apiVersion: networking.istio.io/v1beta1
  kind: WorkloadEntry
  metadata:
    annotations:
      sidecar-bootstrap.istioctl.istio.io/proxy-config-dir: /etc/istio-proxy # Directory on the remote node to copy generated files into
      sidecar-bootstrap.istioctl.istio.io/ssh-user: istio-proxy              # User to SSH as; must have permissions to run Docker commands
                                                                             # and to write copied files into the target directory
      sidecar.istio.io/statsInclusionRegexps: ".*"                           # Configure Envoy proxy to export all available stats
      proxy.istio.io/config: |
        concurrency: 3                                                       # ProxyConfig overrides to apply
    name: my-vm
    namespace: my-namespace
  spec:
    address: 1.2.3.4                                                         # At runtime, Istio Sidecar will bind incoming listeners to that address.
                                                                             # At bootstrap time, this command will SSH to that address
    labels:
      app: ratings
      version: v1
      class: vm                                                              # It's very handy to have extra labels on a WorkloadEntry
                                                                             # to be able to narrow down label selectors to VM workloads only
    network: on-premise                                                      # If your VM doesn't have L3 connectivity to k8s Pods,
                                                                             # make sure to fill in network field
    serviceAccount: ratings-sa

For a complete list of supported annotations run '%[2]s sidecar-bootstrap --docs'.`, bootstrapAnnotation.ScpPath.Name, opts.ParentCommandDocPath),
		Example: fmt.Sprintf(`  # Show under-the-hood actions to copy workload identity of a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace --dry-run

  # Show under-the-hood actions to copy workload identity and start Istio Sidecar on a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace --start-istio-proxy --dry-run

  # Copy workload identity into a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace

  # Copy workload identity and start Istio Sidecar on a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace --start-istio-proxy

  # Generate workload identity for a VM represented by a given WorkloadEntry and save generated files locally
  %[1]s sidecar-bootstrap my-vm.my-namespace --local-dir path/to/save/workload/identity

  # Print a list of supported annotations on the WorkloadEntry resource:
  %[1]s sidecar-bootstrap --docs`, opts.ParentCommandDocPath),
		Args: func(cmd *cobra.Command, args []string) error {
			if printDocs {
				return nil
			}
			if len(args) == 0 && !all {
				return fmt.Errorf("sidecar-bootstrap command requires either a <workload-entry-name>[.<namespace>] argument or the --all flag")
			}
			if len(args) > 0 && all {
				return fmt.Errorf("sidecar-bootstrap command requires either a <workload-entry-name>[.<namespace>] argument or the --all flag but not both")
			}
			if defaultSSHUser == "" {
				user, err := user.Current()
				if err != nil {
					return fmt.Errorf("failed to determine current user: %w", err)
				}
				defaultSSHUser = user.Username
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
					prompt := bootstrapSsh.HostKeyPrompt(cmd.InOrStdin(), cmd.ErrOrStderr())
					homeDir, err := homedir.Dir()
					if err != nil {
						return fmt.Errorf("failed to determine home directory of the current user: %w", err)
					}
					filename := filepath.Join(homeDir, ".ssh", "known_hosts")
					knownhost, err := knownhosts.New(filename)
					switch {
					case os.IsNotExist(err):
						callback = prompt
					case err != nil:
						return fmt.Errorf("failed to parse %s: %w", filename, err)
					default:
						callback = bootstrapSsh.HostKeyCallbackChain(knownhost, prompt)
					}
				}

				sshConfig = ssh.ClientConfig{
					User:            defaultSSHUser,
					Auth:            []ssh.AuthMethod{sshAuthMethod},
					HostKeyCallback: callback,
					Timeout:         sshConnectTimeout,
				}
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			if printDocs {
				printSidecarBootstrapDocs(c.OutOrStdout(), c.CommandPath())
				return nil
			}

			kubeClient, err := interfaceFactory(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %w", err)
			}

			configClient, err := configStoreFactory()
			if err != nil {
				return fmt.Errorf("failed to create Istio config client: %w", err)
			}

			name, ns := "", handlers.HandleNamespace(namespace, defaultNamespace)
			if len(args) > 0 {
				name, ns = handlers.InferPodInfo(args[0], ns) // reuse logic despite simingly unrelated function name
			}

			var entries []networking.WorkloadEntry
			if name != "" {
				entries, err = fetchSingleWorkloadEntry(configClient, ns, name)
			} else {
				entries, err = fetchAllWorkloadEntries(configClient, ns)
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

			expansionProxyConfig, err := getExpansionProxyConfig(kubeClient, istioNamespace)
			if err != nil {
				return fmt.Errorf("failed to read ProxyConfig for mesh expansion proxies: %w", err)
			}

			if actual, expected := istioConfigValues.GetGlobal().GetJwtPolicy(), "third-party-jwt"; actual != expected {
				return fmt.Errorf("jwt policy is set to %q. At the moment, %q command only supports jwt policy %q", actual, c.CommandPath(), expected)
			}

			if actual, expected := istioConfigValues.GetGlobal().GetPilotCertProvider(), "istiod"; actual != expected {
				return fmt.Errorf("pilot cert provider is set to %q. At the moment, %q command only supports pilot cert provider %q", actual, c.CommandPath(), expected)
			}

			k8sCaCert, err := getK8sCaCert(kubeClient, ns, istioNamespace)
			if err != nil {
				return fmt.Errorf("unable to find the root cert of a k8s CA: %w", err)
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
				return fmt.Errorf("unable to proceed because Istio Ingress Gateway is not configured for mesh expansion: %w", err)
			}

			istioGatewayAddress, err := getIstioIngressGatewayAddress(ingressSvc)
			if err != nil {
				return fmt.Errorf("unable to find address of the Istio Ingress Gateway: %w", err)
			}

			identities, err := getIdentityForEachWorkload(kubeClient, entries)
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
				ExpansionProxyConfig:       expansionProxyConfig,
			}
			return processWorkloads(entries, identities, data, action)
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
	vmBSCommand.PersistentFlags().BoolVar(&useSSHPassword, "ssh-password", false,
		"(experimental) force SSH password-based authentication")
	vmBSCommand.PersistentFlags().StringVarP(&sshKeyLocation, "ssh-key", "k", "",
		"(experimental) the location of the SSH key")
	vmBSCommand.PersistentFlags().IntVar(&defaultSSHPort, "ssh-port", 22,
		fmt.Sprintf("(experimental) default port to SSH to (is only effective unless the '%s' annotation is present "+
			"on a WorkloadEntry)", bootstrapAnnotation.SSHPort.Name))
	vmBSCommand.PersistentFlags().StringVarP(&defaultSSHUser, "ssh-user", "u", "",
		fmt.Sprintf("(experimental) default user to SSH as, defaults to the current user (is only effective unless "+
			"the '%s' annotation is present on a WorkloadEntry)", bootstrapAnnotation.SSHUser.Name))
	vmBSCommand.PersistentFlags().DurationVar(&sshConnectTimeout, "ssh-connect-timeout", 10*time.Second,
		"(experimental) the maximum amount of time to establish SSH connection")
	vmBSCommand.PersistentFlags().BoolVar(&startIstioProxy, "start-istio-proxy", false,
		"start Istio Sidecar on a remote host after copying workload identity")
	vmBSCommand.PersistentFlags().BoolVar(&dryRun, "dry-run", false,
		"show generated configuration and respective SSH commands but don't connect to, copy files or execute commands remotely")
	vmBSCommand.PersistentFlags().BoolVar(&printDocs, "docs", false,
		"(experimental) print supported annotations on the WorkloadEntry resource")

	// same options as in `istioctl inject`
	vmBSCommand.PersistentFlags().StringVar(&meshConfigMapName, "meshConfigMapName", defaultMeshConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio mesh configuration, key should be %q", configMapKey))
	vmBSCommand.PersistentFlags().StringVar(&injectConfigMapName, "injectConfigMapName", defaultInjectConfigMapName,
		fmt.Sprintf("ConfigMap name for Istio sidecar injection, key should be %q", injectConfigMapKey))

	return vmBSCommand
}

func printSidecarBootstrapDocs(out io.Writer, cmd string) {
	format := func(item *annotation.Instance) {
		fmt.Fprintf(out, "* %s\n\n", item.Name)

		scanner := bufio.NewScanner(strings.NewReader(item.Description))
		for scanner.Scan() {
			fmt.Fprintf(out, "    %s\n", scanner.Text())
		}
		fmt.Fprintf(out, "\n")
	}

	fmt.Fprintf(out, "List of annotations on a WorkloadEntry resource supported by the %q command:\n\n", cmd)

	fmt.Fprintf(out, "Standard Istio annotations:\n\n")
	for _, item := range bootstrapAnnotation.SupportedIstioAnnotations() {
		format(item)
	}

	fmt.Fprintf(out, "Annotations specific to %q command:\n\n", cmd)
	for _, item := range bootstrapAnnotation.SupportedCustomAnnotations() {
		format(item)
	}
}
