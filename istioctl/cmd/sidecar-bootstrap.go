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
	"archive/tar"
	"bufio"
	"compress/gzip"
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
	"istio.io/istio/istioctl/pkg/help/markdown"
	"istio.io/istio/istioctl/pkg/util/handlers"
	istioconfig "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	meshNetworksConfigMapKey = "meshNetworks"
)

type BootstrapBundle = bootstrapBundle.BootstrapBundle
type SidecarData = bootstrapBundle.SidecarData

var resourceURI = bootstrapUtil.ResourceURI

var (
	// overridable by unit tests
	now                                  = time.Now
	vmBootstrapCmd func() *cobra.Command = defaultVMBootstrapCmd
)

const (
	defaultProxyConfigDir    = "/tmp/istio-proxy" // the most reliable default value for out-of-the-box experience
	offlineProxyConfigDirEnv = "VM_FILES_DIR"     // env variable used in scripts for offline onboarding
)

var (
	dryRun            bool
	all               bool
	tokenDuration     time.Duration
	outputDir         string
	outputArchive     bool
	outputArchivePath string
	defaultSSHPort    int
	defaultSSHUser    string
	sshConnectTimeout time.Duration
	useSSHPassword    bool
	sshKeyLocation    string
	sshIgnoreHostKeys bool
	defaultScpOpts    = bootstrapSsh.CopyOpts{
		RemoteScpPath: "/usr/bin/scp",
	}
	startIstioProxy bool
	printDocs       bool
)

type workloadIdentity struct {
	ServiceAccountToken []byte
}

type fileToCopy struct {
	name string
	dir  string
	perm os.FileMode
	data []byte
}

type cmdToExec struct {
	cmd      string
	required bool
}

type bootstrapItems struct {
	// Files to copy to the VM
	filesToCopy []fileToCopy
	// Commands to execute on the VM (order is important).
	cmdsToExec []cmdToExec
}

type sshParams struct {
	address  string
	username string
	scp      bootstrapSsh.CopyOpts
	client   bootstrapSsh.Client
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

func getMeshNetworksFromConfigMap(kubeconfig, command string) (*meshconfig.MeshNetworks, error) {
	client, err := interfaceFactory(kubeconfig)
	if err != nil {
		return nil, err
	}

	meshConfigMap, err := client.CoreV1().ConfigMaps(istioNamespace).Get(context.TODO(), meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not read valid configmap %q from namespace %q: %v - "+
			"Use --meshConfigFile or re-run "+command+" with `-i <istioSystemNamespace> and ensure valid MeshConfig exists",
			meshConfigMapName, istioNamespace, err)
	}
	// values in the data are strings, while proto might use a
	// different data type.  therefore, we have to get a value by a
	// key
	configYaml, exists := meshConfigMap.Data[meshNetworksConfigMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", meshNetworksConfigMapKey)
	}
	cfg, err := mesh.ParseMeshNetworks(configYaml)
	if err != nil {
		err = multierror.Append(err, fmt.Errorf("istioctl version %s cannot parse mesh config.  Install istioctl from the latest Istio release",
			version.Info.Version))
	}
	return cfg, err
}

func getGlobalProxyConfigOverrides(kubeClient kubernetes.Interface, namespace, configmap string) (string, error) {
	cm, err := kubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), configmap, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read ConfigMap %s that holds global config overrides for mesh expansion proxies: %w",
			resourceURI("v1", "configmaps", namespace, configmap), err)
	}
	value := cm.Data["PROXY_CONFIG"]
	if value == "" {
		return "", nil
	}
	proxyConfig := new(meshconfig.ProxyConfig)
	if err := gogoprotomarshal.ApplyYAML(value, proxyConfig); err != nil {
		return "", fmt.Errorf("failed to unmarshal ProxyConfig from the ConfigMap %s "+
			"that holds global config overrides for mesh expansion proxies: %w",
			resourceURI("v1", "configmaps", cm.Namespace, cm.Name), err)
	}
	return value, nil
}

func getProxyConfigOverrides(kubeClient kubernetes.Interface, namespace string) (string, error) {
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

func getIstioIngressGatewayAddress(kubeClient kubernetes.Interface, istioNamespace string,
	meshConfig *meshconfig.MeshConfig,
	meshNetworksConfig *meshconfig.MeshNetworks,
	istioConfigValues *istioconfig.Values) (string, error) {
	var istioGatewayServiceName, istioGatewayServiceNamespace, istioGatewayServiceSource string
	var istioGatewayAddress string

	if network := meshNetworksConfig.GetNetworks()[istioConfigValues.GetGlobal().GetNetwork()]; network != nil {
		for _, gateway := range network.GetGateways() {
			if svcName := gateway.GetRegistryServiceName(); svcName != "" && istioGatewayServiceName == "" {
				istioGatewayServiceName, istioGatewayServiceNamespace = bootstrapBundle.SplitServiceAndNamespace(svcName, "")
				istioGatewayServiceSource = "MeshNetworks"
			}
			if address := gateway.GetAddress(); address != "" && istioGatewayAddress == "" {
				istioGatewayAddress = address
			}
		}
	}

	if istioGatewayServiceName == "" && istioGatewayAddress == "" {
		if value := meshConfig.GetIngressService(); value != "" {
			istioGatewayServiceName, istioGatewayServiceNamespace = value, istioNamespace
			istioGatewayServiceSource = "MeshConfig.IngressService"
		} else {
			istioGatewayServiceName, istioGatewayServiceNamespace = "istio-ingressgateway", istioNamespace // fallback value according to Istio docs
			istioGatewayServiceSource = "default"
		}
	}

	if istioGatewayServiceName != "" {
		ingressSvc, err := getIstioIngressGatewayService(kubeClient, istioGatewayServiceNamespace, istioGatewayServiceName)
		if err != nil {
			return "", fmt.Errorf("unable to find Istio Ingress Gateway inferred from %s settings: %w", istioGatewayServiceSource, err)
		}

		if err := verifyMeshExpansionPorts(ingressSvc); err != nil {
			return "", fmt.Errorf("it appears that Istio Ingress Gateway inferred from %s settings is not configured for mesh expansion: %w",
				istioGatewayServiceSource, err)
		}

		istioGatewayAddress, err = getLoadBalancerAddress(ingressSvc)
		if err != nil {
			return "", fmt.Errorf("unable to determine address of the Istio Ingress Gateway inferred from %s settings: %w",
				istioGatewayServiceSource, err)
		}
	}

	if istioGatewayAddress == "" {
		return "", fmt.Errorf("unable to infer address of the Istio Ingress Gateway neither from MeshNetworks, nor from MeshConfig," +
			" nor from default settings")
	}

	return istioGatewayAddress, nil
}

func getIstioIngressGatewayService(kubeClient kubernetes.Interface, namespace, service string) (*corev1.Service, error) {
	svc, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Service %s: %w", resourceURI("v1", "services", namespace, service), err)
	}
	return svc, nil
}

func verifyMeshExpansionPorts(svc *corev1.Service) error {
	actualPorts := make(map[int32]bool)
	for _, port := range svc.Spec.Ports {
		actualPorts[port.Port] = true
	}
	meshExpansionPorts := []struct {
		port        int32
		description string
	}{
		{port: 15443, description: "TLS AUTO_PASSTHROUGH"},
	}
	for _, expected := range meshExpansionPorts {
		if present := actualPorts[expected.port]; !present {
			return fmt.Errorf("mesh expansion is not possible because Istio Ingress Gateway Service %s is missing a port '%d (%s)'",
				resourceURI("v1", "services", svc.Namespace, svc.Name), expected.port, expected.description)
		}
	}
	return nil
}

func getLoadBalancerAddress(svc *corev1.Service) (string, error) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return "", fmt.Errorf("k8s Service %s has no ingress points", resourceURI("v1", "services", svc.Namespace, svc.Name))
	}
	// prefer ingress point with DNS name
	for _, endpoint := range svc.Status.LoadBalancer.Ingress {
		if value := endpoint.Hostname; value != "" {
			return value, nil
		}
	}
	// fallback to ingress point with IP
	for _, endpoint := range svc.Status.LoadBalancer.Ingress {
		if value := endpoint.IP; value != "" {
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

func processBundle(bundle BootstrapBundle, remoteDir string) bootstrapItems {
	var files []fileToCopy

	configFilePerm := os.FileMode(0644)
	secretFilePerm := os.FileMode(0640)

	files = append(files,
		fileToCopy{
			name: "sidecar.env",
			dir:  remoteDir,
			perm: configFilePerm,
			data: bundle.IstioProxyEnvironment,
		},
		fileToCopy{
			name: "istio-ca.pem",
			dir:  remoteDir,
			perm: configFilePerm,
			data: bundle.IstioCaCert,
		},
		fileToCopy{
			name: "istio-token",
			dir:  remoteDir,
			perm: secretFilePerm,
			data: bundle.ServiceAccountToken,
		},
	)

	var commands []cmdToExec
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
		remoteDir + "/istio-ca.pem" + ":" + "/var/run/secrets/istio/root-cert.pem",
		"-v",
		// "./var/run/secrets/tokens/istio-token" is a hardcoded value in `istio-agent` that corresponds to `JWT_POLICY == third-party-jwt`
		remoteDir + "/istio-token" + ":" + "/var/run/secrets/tokens/istio-token",
		"--env-file",
		remoteDir + "/sidecar.env",
	}

	for _, host := range bundle.IstioProxyHosts {
		cmd = append(cmd,
			"--add-host",
			host+":"+bundle.IstioIngressGatewayAddress,
		)
	}
	cmd = append(cmd, bundle.IstioProxyImage)
	cmd = append(cmd, bundle.IstioProxyArgs...)

	commands = append(commands,
		cmdToExec{
			cmd:      fmt.Sprintf("docker rm --force %s", bundle.IstioProxyContainerName),
			required: false,
		},
		cmdToExec{
			cmd:      strings.Join(cmd, " "),
			required: true,
		},
	)

	return bootstrapItems{filesToCopy: files, cmdsToExec: commands}
}

func writeBootstrapBundle(bundle BootstrapBundle, write func(filename string, perm os.FileMode, content []byte) error) error {
	items := processBundle(bundle, fmt.Sprintf(`"${%s}"`, offlineProxyConfigDirEnv))

	// write config files
	for _, file := range items.filesToCopy {
		if err := write(path.Join("etc", file.name), file.perm, file.data); err != nil {
			return err
		}
	}

	// wrap individual commands into a start up script
	content := fmt.Sprintf(`#!/usr/bin/env bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

SCRIPT_DIR=$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )

BASE_DIR="${SCRIPT_DIR}/.."

%s="${BASE_DIR}/etc"

`, offlineProxyConfigDirEnv)
	for _, command := range items.cmdsToExec {
		if command.required {
			content += "set -e\n"
		} else {
			content += "set +e\n"
		}
		content += command.cmd + "\n\n"
	}

	// write start up script
	if err := write(path.Join("bin", "start-istio-proxy.sh"), os.FileMode(0755), []byte(content)); err != nil {
		return err
	}
	return nil
}

func copyBootstrapBundle(sshConfig ssh.ClientConfig, ssh sshParams, items bootstrapItems) error {
	err := ssh.client.Dial(ssh.address, ssh.username, sshConfig)
	if err != nil {
		return err
	}
	defer ssh.client.Close()

	// Copy all files to the VM.
	dirs := make(map[string]bool)
	for _, file := range items.filesToCopy {
		if created := dirs[file.dir]; !created {
			// Ensure the remote directory exists.
			err = ssh.client.Exec("mkdir -p " + file.dir)
			if err != nil {
				return err
			}
			dirs[file.dir] = true
		}

		err = ssh.client.Copy(file.data, path.Join(file.dir, file.name), file.perm, ssh.scp)
		if err != nil {
			return err
		}
	}

	if startIstioProxy {
		for _, command := range items.cmdsToExec {
			if err := ssh.client.Exec(command.cmd); err != nil {
				if command.required {
					return err
				}
				log.Warna(err)
			}
		}
	}
	return nil
}

func parseSSHConfig(stdin io.Reader, stderr io.Writer) (*ssh.ClientConfig, error) {
	if defaultSSHUser == "" {
		user, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("failed to determine current user: %w", err)
		}
		defaultSSHUser = user.Username
	}
	sshConfig := ssh.ClientConfig{
		Timeout: sshConnectTimeout,
	}
	if dryRun {
		return &sshConfig, nil // don't force users to provide SSH credentials in dry run mode
	}
	authMethod, err := deriveSSHMethod(stdin)
	if err != nil {
		return nil, err
	}
	sshConfig.Auth = []ssh.AuthMethod{authMethod}
	var callback ssh.HostKeyCallback
	if sshIgnoreHostKeys {
		callback = ssh.InsecureIgnoreHostKey()
	} else {
		prompt := bootstrapSsh.HostKeyPrompt(stdin, stderr)
		homeDir, err := homedir.Dir()
		if err != nil {
			return nil, fmt.Errorf("failed to determine home directory of the current user: %w", err)
		}
		filename := filepath.Join(homeDir, ".ssh", "known_hosts")
		knownhost, err := knownhosts.New(filename)
		switch {
		case os.IsNotExist(err):
			callback = prompt
		case err != nil:
			return nil, fmt.Errorf("failed to parse %s: %w", filename, err)
		default:
			callback = bootstrapSsh.HostKeyCallbackChain(knownhost, prompt)
		}
	}
	sshConfig.HostKeyCallback = callback
	return &sshConfig, nil
}

func deriveSSHMethod(stdin io.Reader) (_ ssh.AuthMethod, errs error) {
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

		rawModeStdin, restoreStdin, err := bootstrapUtil.RawModeStdin(stdin)
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
			rawModeStdin, restoreStdin, err := bootstrapUtil.RawModeStdin(stdin)
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
			return nil, err
		}
		return ssh.Password(sshPassword), nil
	}

	var candidateKeyLocations []string
	if sshKeyLocation != "" {
		candidateKeyLocations = []string{sshKeyLocation}
	} else {
		homeDir, err := homedir.Dir()
		if err != nil {
			return nil, fmt.Errorf("failed to determine home directory of the current user: %w", err)
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
		return ssh.PublicKeys(key), nil
	}
	return nil, errs
}

func writeToDir(outputDir string, multiEntry bool,
	workloads []networking.WorkloadEntry, identities map[string]workloadIdentity, data *SidecarData) error {
	action := func(bundle BootstrapBundle) error {
		bundleDir := outputDir
		if multiEntry {
			bundleDir = filepath.Join(outputDir, bundle.Workload.Name)
		}
		err := os.MkdirAll(bundleDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to create the output directory at %q: %w", bundleDir, err)
		}
		writeFunc := func(filename string, perm os.FileMode, content []byte) error {
			filename = filepath.Join(bundleDir, filename)
			dir := filepath.Dir(filename)
			err := os.MkdirAll(dir, os.ModePerm)
			if err != nil {
				return fmt.Errorf("failed to create a directory at %q: %w", dir, err)
			}
			err = ioutil.WriteFile(filename, content, perm)
			if err != nil {
				return fmt.Errorf("failed to write to a file %q: %w", filename, err)
			}
			return nil
		}
		return writeBootstrapBundle(bundle, writeFunc)
	}
	return processWorkloads(workloads, identities, data, action)
}

func pickArchiveName(prefix string) (time.Time, string, error) {
	maxAttempts := 15
	for i := 0; i < maxAttempts; i++ {
		creationTime := now()
		outputPath := fmt.Sprintf("%s.%s.tgz", prefix, creationTime.Format("20060102150405"))
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			return creationTime, outputPath, nil
		}
		time.Sleep(1 * time.Second)
	}
	return time.Time{}, "", fmt.Errorf("failed to automatically pick a name for the TGZ file after %d attempts", maxAttempts)
}

func writeToArchive(outputPath string, multiEntry bool, creationTime time.Time,
	workloads []networking.WorkloadEntry, identities map[string]workloadIdentity, data *SidecarData) error {
	dir := filepath.Dir(outputPath)
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create a directory with the output archive at %q: %w", dir, err)
	}
	outputFile, err := os.OpenFile(outputPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", outputPath, err)
	}
	defer outputFile.Close()

	gzipWriter, err := gzip.NewWriterLevel(outputFile, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("failed to create gzip encoder %q: %w", outputPath, err)
	}
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	action := func(bundle BootstrapBundle) error {
		bundleDir := ""
		if multiEntry {
			bundleDir = bundle.Workload.Name
		}

		writeFunc := func(filename string, perm os.FileMode, content []byte) error {
			filename = path.Join(bundleDir, filename)
			header := &tar.Header{
				Name:    filename,
				Mode:    int64(perm),
				Size:    int64(len(content)),
				ModTime: creationTime,
			}
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to add a file header %q to the TAR archive: %w", filename, err)
			}
			if _, err := tarWriter.Write(content); err != nil {
				return fmt.Errorf("failed to write file contents %q to the TAR archive: %w", filename, err)
			}
			return nil
		}
		return writeBootstrapBundle(bundle, writeFunc)
	}
	return processWorkloads(workloads, identities, data, action)
}

func copyOverSSH(sshConfig *ssh.ClientConfig, stdout io.Writer, stderr io.Writer,
	workloads []networking.WorkloadEntry, identities map[string]workloadIdentity, data *SidecarData) error {
	action := func(bundle BootstrapBundle) error {
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
		scpOpts := defaultScpOpts
		if value := bundle.Workload.Annotations[bootstrapAnnotation.ScpPath.Name]; value != "" {
			scpOpts.RemoteScpPath = value
		}
		sshClient := sshClientFactory(stdout, stderr)
		sshParams := sshParams{
			address:  address,
			username: username,
			client:   sshClient,
			scp:      scpOpts,
		}
		remoteDir := defaultProxyConfigDir
		if value := bundle.Workload.Annotations[bootstrapAnnotation.ProxyConfigDir.Name]; value != "" {
			remoteDir = value
		}
		return copyBootstrapBundle(*sshConfig, sshParams, processBundle(bundle, remoteDir))
	}
	return processWorkloads(workloads, identities, data, action)
}

type VMBootstrapCommandOpts struct {
	// ParentCommandDocPath is a full path of the parent command that should be used in help messages.
	//
	// By default, "istioctl x" is assumed.
	ParentCommandDocPath string

	// GlobalProxyConfigOverridesConfigMap specifies a custom ConfigMap with configuration that
	// 1) will override mesh-wide default ProxyConfig
	// 2) will be overridden by user-defined ProxyConfig overrides for all mesh expansion proxies
	// 3) will be overridden by user-defined ProxyConfig overrides for a particual mesh expansion proxy
	GlobalProxyConfigOverridesConfigMap string
}

func defaultVMBootstrapCmd() *cobra.Command {
	return NewVMBootstrapCommand(VMBootstrapCommandOpts{})
}

func NewVMBootstrapCommand(opts VMBootstrapCommandOpts) *cobra.Command {
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
If you want to use an SSH Password or a passphrase-protected SSH Key, you must run this command on an interactive terminal to type the password in.
We do not accept passwords through command line options to avoid leaking secrets into shell history.

File copying is performed over SCP protocol, and as such SCP binary must be installed on the remote node.
If SCP is installed in a location other than %[1]s, you have to provide absolute path to the SCP binary
by adding %[2]s annotation to the respective WorkloadEntry resource.

To start Istio Sidecar on the remote node you must have Docker installed there.
Istio Sidecar will be started on the host network as a docker container in capture mode.

While this command can work without any explicit configuration, it is also possible to fine tune its behavior
by adding various annotations on a WorkloadEntry resource. E.g., consider the following real life example:

`+markdown.CodeBlock("yaml", "  ", `  apiVersion: networking.istio.io/v1beta1
  kind: WorkloadEntry
  metadata:
    annotations:
      sidecar-bootstrap.istio.io/proxy-config-dir: /etc/istio-proxy # Directory on the remote node to copy generated files into
      sidecar-bootstrap.istio.io/ssh-user: istio-proxy              # User to SSH as; must have permissions to run Docker commands
                                                                    # and to write copied files into the target directory
      sidecar.istio.io/statsInclusionRegexps: ".*"                  # Configure Envoy proxy to export all available stats
      proxy.istio.io/config: |
        concurrency: 3                                              # ProxyConfig overrides to apply
    name: my-vm
    namespace: my-namespace
  spec:
    address: 1.2.3.4                                                # At runtime, Istio Sidecar will bind incoming listeners to that address.
                                                                    # At bootstrap time, this command will SSH to that address
    labels:
      app: ratings
      version: v1
      class: vm                                                     # It's very handy to have extra labels on a WorkloadEntry
                                                                    # to be able to narrow down label selectors to VM workloads only
    network: on-premise                                             # If your VM doesn't have L3 connectivity to k8s Pods,
                                                                    # make sure to fill in network field
    serviceAccount: ratings-sa`)+`

For a complete list of supported annotations run %[3]s.`,
			markdown.InlineCode("/usr/bin/scp"),
			markdown.InlineCode(bootstrapAnnotation.ScpPath.Name),
			markdown.InlineCode(fmt.Sprintf("%s sidecar-bootstrap --docs", opts.ParentCommandDocPath))),
		Example: fmt.Sprintf(`  # Show under-the-hood actions to copy workload identity of a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace --dry-run

  # Show under-the-hood actions to copy workload identity and start Istio Sidecar on a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace --start-istio-proxy --dry-run

  # Copy workload identity into a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace

  # Copy workload identity and start Istio Sidecar on a VM represented by a given WorkloadEntry:
  %[1]s sidecar-bootstrap my-vm.my-namespace --start-istio-proxy

  # Generate workload identity for a VM represented by a given WorkloadEntry and save generated files into an archive file (*.tgz) at a given path
  %[1]s sidecar-bootstrap my-vm.my-namespace --output-file path/to/output/file.tgz

  # Generate workload identity for a VM represented by a given WorkloadEntry and save generated files into a directory
  %[1]s sidecar-bootstrap my-vm.my-namespace --output-dir path/to/output/dir

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
			if cmd.Flags().ShorthandLookup("o").Changed && cmd.Flags().Lookup("output-file").Changed {
				return fmt.Errorf("use either -o or --output-file but not both")
			}
			if cmd.Flags().ShorthandLookup("o").Changed && cmd.Flags().Lookup("output-dir").Changed {
				return fmt.Errorf("use either -o or --output-dir but not both")
			}
			if cmd.Flags().Lookup("output-file").Changed && cmd.Flags().Lookup("output-dir").Changed {
				return fmt.Errorf("use either --output-file or --output-dir but not both")
			}
			if cmd.Flags().ShorthandLookup("o").Changed && cmd.Flags().Lookup("dry-run").Changed {
				return fmt.Errorf("it is not possible to use --dry-run flag together with -o")
			}
			if cmd.Flags().Lookup("output-file").Changed && cmd.Flags().Lookup("dry-run").Changed {
				return fmt.Errorf("it is not possible to use --dry-run flag together with --output-file")
			}
			if cmd.Flags().Lookup("output-dir").Changed && cmd.Flags().Lookup("dry-run").Changed {
				return fmt.Errorf("it is not possible to use --dry-run flag together with --output-dir")
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

			_, err = kubeClient.Discovery().ServerVersion() // to avoid confusing error messages later on, check connectivity to k8s in the beginning
			if err != nil {
				return fmt.Errorf(`unable to access k8s API: %w

Hint: make sure that "kubectl" or "istioctl" run successfully in this environment;
      you might have forgotten to switch k8s context or your authentication might have expired

      E.g., check whether the following command succeeds:

        kubectl version`, err)
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
			if len(entries) == 0 {
				fmt.Fprintf(c.ErrOrStderr(), "There are no WorkloadEntry(s) to bootstrap\n")
				return nil
			}

			meshConfig, err := getMeshConfigFromConfigMap(kubeconfig, c.CommandPath())
			if err != nil {
				return fmt.Errorf("failed to read Istio Mesh configuration: %w", err)
			}

			meshNetworksConfig, err := getMeshNetworksFromConfigMap(kubeconfig, c.CommandPath())
			if err != nil {
				return fmt.Errorf("failed to read Istio Mesh Networks configuration: %w", err)
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

			istioCaCert, err := getIstioCaCert(kubeClient, istioNamespace)
			if err != nil {
				return fmt.Errorf("unable to find Istio CA cert: %w", err)
			}

			istioGatewayAddress, err := getIstioIngressGatewayAddress(kubeClient, istioNamespace, meshConfig, meshNetworksConfig, istioConfigValues)
			if err != nil {
				return fmt.Errorf("unable to proceed because mesh expansion is either disabled or misconfigured: %w", err)
			}

			globalProxyConfigOverrides := ""
			if opts.GlobalProxyConfigOverridesConfigMap != "" {
				globalProxyConfigOverrides, err = getGlobalProxyConfigOverrides(kubeClient, istioNamespace, opts.GlobalProxyConfigOverridesConfigMap)
				if err != nil {
					return fmt.Errorf("failed to read ProxyConfig overrides for mesh expansion proxies: %w", err)
				}
			}

			userProxyConfigOverrides, err := getProxyConfigOverrides(kubeClient, istioNamespace)
			if err != nil {
				return fmt.Errorf("failed to read ProxyConfig overrides for mesh expansion proxies: %w", err)
			}

			identities, err := getIdentityForEachWorkload(kubeClient, entries)
			if err != nil {
				return fmt.Errorf("failed to generate security token(s) for WorkloadEntry(s): %w", err)
			}

			data := &SidecarData{
				IstioSystemNamespace:       istioNamespace,
				IstioMeshConfig:            meshConfig,
				IstioConfigValues:          istioConfigValues,
				IstioCaCert:                istioCaCert,
				IstioIngressGatewayAddress: istioGatewayAddress,
				ProxyConfigOverrides:       []string{globalProxyConfigOverrides, userProxyConfigOverrides},
			}

			multiEntry := len(args) == 0

			if outputDir != "" {
				err := writeToDir(outputDir, multiEntry, entries, identities, data)
				if err != nil {
					return nil
				}
				if abs, err := filepath.Abs(outputDir); err == nil {
					outputDir = abs
				}
				fmt.Fprintf(c.ErrOrStderr(), `Generated files have been saved to the directory `+markdown.InlineCode(outputDir)+`

Next steps:

  1. Copy the contents of `+markdown.InlineCode(outputDir)+` directory to the remote host represented by the WorkloadEntry

  2. Once on the remote host, run `+markdown.InlineCode("<dir>/bin/start-istio-proxy.sh")+` to start Istio Proxy in a Docker container
`)
			} else if outputArchive || outputArchivePath != "" {
				creationTime := now()
				outputPath := outputArchivePath
				if outputPath == "" {
					var prefix string
					if multiEntry {
						prefix = ns
					} else {
						prefix = strings.Join([]string{name, ns}, ".")
					}
					creationTime, outputPath, err = pickArchiveName(prefix)
					if err != nil {
						return err
					}
				}
				err := writeToArchive(outputPath, multiEntry, creationTime, entries, identities, data)
				if err != nil {
					return nil
				}
				if abs, err := filepath.Abs(outputPath); err == nil {
					outputPath = abs
				}
				fmt.Fprintf(c.ErrOrStderr(), `Generated files have been saved into the TGZ archive `+markdown.InlineCode(outputPath)+`

Next steps:

  1. Copy the file `+markdown.InlineCode(outputPath)+` to the remote host represented by the WorkloadEntry

  2. Once on the remote host,

     1. run `+markdown.InlineCode("tar -xvf "+filepath.Base(outputPath))+` to extract archive into the working directory

     2. run `+markdown.InlineCode("./bin/start-istio-proxy.sh")+` to start Istio Proxy in a Docker container
`)
			} else {
				sshConfig, err := parseSSHConfig(c.InOrStdin(), c.ErrOrStderr())
				if err != nil {
					return err
				}
				return copyOverSSH(sshConfig, c.OutOrStdout(), c.ErrOrStderr(), entries, identities, data)
			}
			return nil
		},
	}

	vmBSCommand.PersistentFlags().BoolVarP(&all, "all", "a", false,
		"bootstrap all WorkloadEntry(s) in a given namespace")
	vmBSCommand.PersistentFlags().DurationVar(&tokenDuration, "duration", 24*time.Hour,
		"(experimental) amount of time that generated ServiceAccount tokens should be valid for")
	vmBSCommand.PersistentFlags().StringVarP(&outputDir, "output-dir", "d", "",
		"save generated files into a local directory instead of copying them to a remote machine")
	vmBSCommand.PersistentFlags().BoolVarP(&outputArchive, "archive", "o", false,
		"(experimental) save generated files into a local archive file (*.tgz) instead of copying them to a remote machine "+
			"(file name will be picked automatically)")
	vmBSCommand.PersistentFlags().StringVar(&outputArchivePath, "output-file", "",
		"(experimental) save generated files into a local archive file (*.tgz) instead of copying them to a remote machine "+
			"(file name is picked by the user)")
	vmBSCommand.PersistentFlags().DurationVar(&defaultScpOpts.Timeout, "timeout", 60*time.Second,
		"(experimental) timeout on copying a single file to a remote host")
	vmBSCommand.PersistentFlags().BoolVar(&sshIgnoreHostKeys, "ignore-host-keys", false,
		"(experimental) do not verify remote host key when establishing SSH connection")
	vmBSCommand.PersistentFlags().BoolVar(&useSSHPassword, "ssh-password", false,
		"(experimental) force SSH password-based authentication")
	vmBSCommand.PersistentFlags().StringVarP(&sshKeyLocation, "ssh-key", "k", "",
		"(experimental) authenticate with SSH key at a given location")
	vmBSCommand.PersistentFlags().IntVar(&defaultSSHPort, "ssh-port", 22,
		fmt.Sprintf("(experimental) default port to SSH to (is only effective unless the '%s' annotation is present "+
			"on a WorkloadEntry)", bootstrapAnnotation.SSHPort.Name))
	vmBSCommand.PersistentFlags().StringVarP(&defaultSSHUser, "ssh-user", "u", "",
		fmt.Sprintf("(experimental) default user to SSH as, defaults to the current user (is only effective unless "+
			"the '%s' annotation is present on a WorkloadEntry)", bootstrapAnnotation.SSHUser.Name))
	vmBSCommand.PersistentFlags().DurationVar(&sshConnectTimeout, "ssh-connect-timeout", 10*time.Second,
		"(experimental) timeout on establishing SSH connection")
	vmBSCommand.PersistentFlags().BoolVar(&startIstioProxy, "start-istio-proxy", false,
		"start Istio Sidecar on a remote host after copying configuration files")
	vmBSCommand.PersistentFlags().BoolVar(&dryRun, "dry-run", false,
		"print generated configuration and respective SSH commands but don't connect to, copy files or execute commands remotely")
	vmBSCommand.PersistentFlags().BoolVar(&printDocs, "docs", false,
		"(experimental) print a list of supported annotations on the WorkloadEntry resource")

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

	fmt.Fprintf(out, "List of annotations on a WorkloadEntry resource supported by the %s command:\n\n", markdown.InlineCode(cmd))

	fmt.Fprintf(out, "Standard Istio annotations:\n\n")
	for _, item := range bootstrapAnnotation.SupportedIstioAnnotations() {
		format(item)
	}

	fmt.Fprintf(out, "Annotations specific to %s command:\n\n", markdown.InlineCode(cmd))
	for _, item := range bootstrapAnnotation.SupportedCustomAnnotations() {
		format(item)
	}
}
