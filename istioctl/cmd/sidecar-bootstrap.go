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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"sync"
	"time"

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
	istioconfig "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/log"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	sidecarBootstrapSshHostAnnotation = "sidecar-bootstrap.istioctl.istio.io/ssh-host"
	sidecarBootstrapSshPortAnnotation = "sidecar-bootstrap.istioctl.istio.io/ssh-port"
	sidecarBootstrapSshUserAnnotation = "sidecar-bootstrap.istioctl.istio.io/ssh-user"
)

var (
	all               bool
	tokenDuration     time.Duration
	dumpDir           string
	remoteDirectory   string
	scpPath           string
	scpTimeout        time.Duration
	sshConnectTimeout time.Duration
	sshAuthMethod     ssh.AuthMethod
	sshKeyLocation    string
	sshIgnoreHostKeys bool
	sshPort           int
	sshUser           string
	startIstio        bool
	dryRun            bool
)

type workloadIdentity struct {
	ServiceAccountToken []byte
}

type sidecarInjectData struct {
	/* k8s */
	K8sCaCert []byte
	/* mesh */
	IstioNamespace             string
	IstioMeshConfig            *meshconfig.MeshConfig
	IstioValues                *istioconfig.Values
	IstioCaCert                []byte
	IstioIngressGatewayAddress string
	/* workload */
	Workload *networking.WorkloadEntry
	/* sidecar */
	ProxyConfig *meshconfig.ProxyConfig
}

type bootstrapBundle struct {
	/* k8s */
	K8sCaCert []byte
	/* mesh */
	IstioCaCert                []byte
	IstioIngressGatewayAddress string
	/* workload */
	Workload            networking.WorkloadEntry
	ServiceAccountToken []byte
	/* sidecar */
	IstioProxyContainerName string
	IstioProxyImage         string
	IstioProxyArgs          []string
	IstioProxyEnvironment   []byte
	IstioProxyHosts         []string
}

type sshClient interface {
	Dial(address, username string) error
	Exec(command string) error
	Copy(data []byte, dstPath string) error
	io.Closer
}

var (
	sshClientFactory = newSshClient
)

func newSshClient(stdout, stderr io.Writer) sshClient {
	if dryRun {
		return newDryRunSshClient(stdout, stderr)
	} else {
		return newRealSshClient(stdout, stderr)
	}
}

func newRealSshClient(stdout, stderr io.Writer) sshClient {
	return &realSshClient{stdout: stdout, stderr: stderr}
}

type realSshClient struct {
	stdout io.Writer
	stderr io.Writer
	client *ssh.Client
}

func (c *realSshClient) Close() error {
	fmt.Fprintf(c.stderr, "[SSH client] closing connection\n")

	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

func newDryRunSshClient(_, stderr io.Writer) sshClient {
	return dryRunSshClient{stderr: stderr}
}

type dryRunSshClient struct {
	stderr io.Writer
}

func (c dryRunSshClient) Dial(address, username string) error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to connect to %s@%s\n", username, address)
	return nil
}

func (c dryRunSshClient) Copy(data []byte, dstPath string) error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to copy into a remote file: %s\n%s\n", dstPath, string(data))
	return nil
}

func (c dryRunSshClient) Exec(command string) error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to execute a command remotely: %s\n", command)
	return nil
}

func (c dryRunSshClient) Close() error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to close connection\n")
	return nil
}

type remoteResponse struct {
	typ     uint8
	message string
}

func fetchSingleWorkloadEntry(client istioclient.Interface, workloadName string) ([]networking.WorkloadEntry, string, error) {
	workloadSplit := strings.Split(workloadName, ".")
	if len(workloadSplit) != 2 {
		return nil, "", fmt.Errorf("workload name %q is not in the format: workloadName.workloadNamespace", workloadName)
	}

	we, err := client.NetworkingV1alpha3().WorkloadEntries(workloadSplit[1]).Get(context.Background(), workloadSplit[0], metav1.GetOptions{})
	if we == nil || err != nil {
		return nil, "", fmt.Errorf("workload entry \"/namespaces/%s/workloadentries/%s\" was not found", workloadSplit[1], workloadSplit[0])
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

func getIstioIngressGatewayAddress(kubeClient kubernetes.Interface, namespace, service string) (string, error) {
	svc, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get Service /namespaces/%s/services/%s: %w", namespace, service, err)
	}
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

func parseRemoteResponse(reader io.Reader) (*remoteResponse, error) {
	buffer := make([]uint8, 1)
	if _, err := reader.Read(buffer); err != nil {
		return nil, err
	}

	typ := buffer[0]
	if typ > 0 {
		buf := bufio.NewReader(reader)
		message, err := buf.ReadString('\n')
		if err != nil {
			return nil, err
		}
		return &remoteResponse{typ, message}, nil
	}

	return &remoteResponse{typ: typ, message: ""}, nil
}

func checkRemoteResponse(r io.Reader) error {
	response, err := parseRemoteResponse(r)
	if err != nil {
		return err
	}

	if response.typ > 0 {
		return errors.New(response.message)
	}

	return nil
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally.
	case <-time.After(timeout):
		return true // timed out.
	}
}

func (c *realSshClient) Dial(address, username string) error {
	fmt.Fprintf(c.stderr, "[SSH client] connecting to %s@%s\n", username, address)

	var callback ssh.HostKeyCallback
	if sshIgnoreHostKeys {
		callback = ssh.InsecureIgnoreHostKey()
	} else {
		user, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to determine current user: %w", err)
		}
		callback, err = knownhosts.New(filepath.Join(user.HomeDir, ".ssh", "known_hosts"))
		if err != nil {
			return fmt.Errorf("failed to create SSH host key callback: %w", err)
		}
	}

	sshConfig := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{sshAuthMethod},
		HostKeyCallback: callback,
		Timeout:         sshConnectTimeout,
	}
	client, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		return fmt.Errorf("failed to estabslish SSH connection: %w", err)
	}
	c.client = client
	return nil
}

func (c *realSshClient) Copy(data []byte, dstPath string) (err error) {
	fmt.Fprintf(c.stderr, "[SSH client] copying into a remote file: %s\n", dstPath)

	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to copy into a remote file %q: %w", dstPath, err)
		}
	}()
	session, err := c.newSession()
	if err != nil {
		return err
	}
	defer session.Close()

	filename := path.Base(dstPath)
	r := bytes.NewReader(data)

	wg := sync.WaitGroup{}
	wg.Add(2)
	errCh := make(chan error, 2)

	size := len(data)

	go func() {
		defer wg.Done()
		w, err := session.StdinPipe()
		if err != nil {
			errCh <- err
			return
		}
		defer w.Close()

		session.Stdout = nil // TODO(yskopets): use io.MultiWriter()
		stdout, err := session.StdoutPipe()
		if err != nil {
			errCh <- err
			return
		}

		// Set the unix file permissions to `0644`.
		//
		// If you don't read unix permissions this correlates to:
		//
		//   Owning User: READ/WRITE
		//   Owning Group: READ
		//   "Other": READ.
		//
		// We keep "OTHER"/"OWNING GROUP" to read so this seemlessly
		// works with the Istio container we start up below.
		_, err = fmt.Fprintln(w, "C0644", size, filename)
		if err != nil {
			errCh <- err
			return
		}

		if err = checkRemoteResponse(stdout); err != nil {
			errCh <- err
			return
		}

		_, err = io.Copy(w, r)
		if err != nil {
			errCh <- err
			return
		}

		_, err = fmt.Fprint(w, "\x00")
		if err != nil {
			errCh <- err
			return
		}

		if err = checkRemoteResponse(stdout); err != nil {
			errCh <- err
			return
		}
	}()

	go func() {
		defer wg.Done()
		err := session.Run(fmt.Sprintf("%s -qt %s", scpPath, dstPath))
		if err != nil {
			errCh <- err
			return
		}
	}()

	if waitTimeout(&wg, scpTimeout) {
		return fmt.Errorf("timeout uploading file")
	}

	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *realSshClient) newSession() (*ssh.Session, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open a new SSH session: %w", err)
	}
	session.Stdout = c.stdout
	session.Stderr = c.stderr
	return session, nil
}

func (c *realSshClient) Exec(command string) (err error) {
	fmt.Fprintf(c.stderr, "[SSH client] executing a command remotely: %s\n", command)

	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to execute a remote command [%s]: %w", command, err)
		}
	}()
	session, err := c.newSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(command)
}

func dumpBootstrapBundle(outputDir string, bundle bootstrapBundle) error {
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

func addressToPodNameAddition(address string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(address)))[0:7]
}

func processWorkloads(kubeClient kubernetes.Interface,
	workloads []networking.WorkloadEntry,
	workloadIdentityMapping map[string]workloadIdentity,
	data *sidecarInjectData,
	handle func(bundle bootstrapBundle) error) error {

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

		bundle := bootstrapBundle{
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

func copyBootstrapBundle(client sshClient, bundle bootstrapBundle) error {
	host := bundle.Workload.Spec.Address
	if value := bundle.Workload.Annotations[sidecarBootstrapSshHostAnnotation]; value != "" {
		host = value
	}
	port := strconv.Itoa(sshPort)
	if value := bundle.Workload.Annotations[sidecarBootstrapSshPortAnnotation]; value != "" {
		port = value
	}
	username := sshUser
	if value := bundle.Workload.Annotations[sidecarBootstrapSshUserAnnotation]; value != "" {
		username = value
	}
	address := net.JoinHostPort(host, port)
	err := client.Dial(address, username)
	if err != nil {
		return err
	}
	defer client.Close()

	// Ensure the remote directory exists.
	err = client.Exec("mkdir -p " + remoteDirectory)
	if err != nil {
		return err
	}

	remoteEnvPath := path.Join(remoteDirectory, "sidecar.env")
	err = client.Copy(bundle.IstioProxyEnvironment, remoteEnvPath)
	if err != nil {
		return err
	}

	remoteK8sCaPath := path.Join(remoteDirectory, "k8s-ca.pem")
	err = client.Copy(bundle.K8sCaCert, remoteK8sCaPath)
	if err != nil {
		return err
	}

	remoteIstioCaPath := path.Join(remoteDirectory, "istio-ca.pem")
	err = client.Copy(bundle.IstioCaCert, remoteIstioCaPath)
	if err != nil {
		return err
	}

	remoteIstioTokenPath := path.Join(remoteDirectory, "istio-token")
	err = client.Copy(bundle.ServiceAccountToken, remoteIstioTokenPath)
	if err != nil {
		return err
	}

	cmd := []string{
		"docker",
		"run",
		"-d",
		"--name",
		bundle.IstioProxyContainerName,
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

	if startIstio {
		if err := client.Exec(fmt.Sprintf("docker rm --force %s", bundle.IstioProxyContainerName)); err != nil {
			log.Warna(err)
		}

		if err := client.Exec(strings.Join(cmd, " ")); err != nil {
			return err
		}
	}
	return nil
}

func deriveSSHMethod() error {
	if sshKeyLocation == "" {
		term := terminal.NewTerminal(os.Stdin, "")
		var err error
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
				term := terminal.NewTerminal(os.Stdin, "")
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
			if (len(args) == 1) == all {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("sidecar-bootstrap requires a WorkloadEntry, or the --all flag")
			}
			if all && namespace == "" {
				return fmt.Errorf("sidecar-bootstrap needs a namespace if fetching all WorkloadEntry(s)")
			}
			if sshUser == "" {
				user, err := user.Current()
				if err != nil {
					return fmt.Errorf("failed to determine current user: %w", err)
				}
				sshUser = user.Username
			}
			if dumpDir == "" && !dryRun {
				err := deriveSSHMethod()
				if err != nil {
					return err
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

			istioValues, err := getConfigValuesFromConfigMap(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to read Istio global values: %w", err)
			}

			if enabled := istioValues.GetGlobal().GetMeshExpansion().GetEnabled().GetValue(); !enabled {
				return fmt.Errorf("mesh expansion is not enabled. Please enable mesh expansion to be able to use %q command", c.CommandPath())
			}

			if actual, expected := istioValues.GetGlobal().GetJwtPolicy(), "third-party-jwt"; actual != expected {
				return fmt.Errorf("jwt policy is set to %q. At the moment, %q command only supports jwt policy %q", actual, c.CommandPath(), expected)
			}

			if actual, expected := istioValues.GetGlobal().GetPilotCertProvider(), "istiod"; actual != expected {
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

			ingressSvc := "istio-ingressgateway" // fallback value according to Istio docs
			if value := meshConfig.GetIngressService(); value != "" {
				ingressSvc = value
			}
			istioGatewayAddress, err := getIstioIngressGatewayAddress(kubeClient, istioNamespace, ingressSvc)
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

			var action func(bundle bootstrapBundle) error
			if dumpDir != "" {
				action = func(bundle bootstrapBundle) error {
					bundleDir := filepath.Join(dumpDir, bundle.Workload.Namespace, bundle.Workload.Name)
					err = os.MkdirAll(bundleDir, os.ModePerm)
					if err != nil && !os.IsExist(err) {
						return fmt.Errorf("failed to create a local output directory %q: %w", bundleDir, err)
					}
					return dumpBootstrapBundle(bundleDir, bundle)
				}
			} else {
				action = func(bundle bootstrapBundle) error {
					sshClient := sshClientFactory(c.OutOrStdout(), c.ErrOrStderr())
					return copyBootstrapBundle(sshClient, bundle)
				}
			}

			data := &sidecarInjectData{
				K8sCaCert:                  k8sCaCert,
				IstioNamespace:             istioNamespace,
				IstioMeshConfig:            meshConfig,
				IstioValues:                istioValues,
				IstioCaCert:                istioCaCert,
				IstioIngressGatewayAddress: istioGatewayAddress,
			}
			return processWorkloads(kubeClient, entries, identities, data, action)
		},
	}

	vmBSCommand.PersistentFlags().BoolVarP(&all, "all", "a", false,
		"attempt to bootstrap all workload entries")
	vmBSCommand.PersistentFlags().DurationVar(&tokenDuration, "duration", 24*time.Hour,
		"(experimental) duration the generated ServiceAccount tokens are valid for.")
	vmBSCommand.PersistentFlags().StringVarP(&dumpDir, "local-dir", "d", "",
		"directory to put workload identities in locally as opposed to copying")
	vmBSCommand.PersistentFlags().StringVar(&remoteDirectory, "remote-directory", "/var/run/istio",
		"(experimental) the directory to create on the remote machine.")
	vmBSCommand.PersistentFlags().StringVar(&scpPath, "remote-scp-path", "/usr/bin/scp",
		"(experimental) the scp binary location on the target machine if not at /usr/bin/scp")
	vmBSCommand.PersistentFlags().DurationVar(&scpTimeout, "timeout", 60*time.Second,
		"(experimental) the timeout for copying workload identities")
	vmBSCommand.PersistentFlags().BoolVar(&sshIgnoreHostKeys, "ignore-host-keys", false,
		"(experimental) ignore host keys on the remote host")
	vmBSCommand.PersistentFlags().StringVarP(&sshKeyLocation, "ssh-key", "k", "",
		"(experimental) the location of the SSH key")
	vmBSCommand.PersistentFlags().IntVar(&sshPort, "ssh-port", 22,
		"(experimental) the port to SSH to the machine on")
	vmBSCommand.PersistentFlags().StringVarP(&sshUser, "ssh-user", "u", "",
		"(experimental) the user to SSH as, defaults to the current user")
	vmBSCommand.PersistentFlags().DurationVar(&sshConnectTimeout, "ssh-connect-timeout", 5*time.Second,
		"(experimental) the maximum amount of time to establish SSH connection")
	vmBSCommand.PersistentFlags().BoolVar(&startIstio, "start-istio-proxy", false,
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

type valueFunc func(data *sidecarInjectData) (string, error)

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
	JWT_POLICY = newEnvVar("JWT_POLICY", func(data *sidecarInjectData) (string, error) {
		return data.IstioValues.GetGlobal().GetJwtPolicy(), nil
	})

	// The provider of Pilot DNS certificate setting implicitly determines
	// the path 'istio-agent' will be looking for the CA cert at:
	//  istiod:     ./var/run/secrets/istio/root-cert.pem
	//  kubernetes: ./var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	//  custom:     ./etc/certs/root-cert.pem
	PILOT_CERT_PROVIDER = newEnvVar("PILOT_CERT_PROVIDER", func(data *sidecarInjectData) (string, error) {
		return data.IstioValues.GetGlobal().GetPilotCertProvider(), nil
	})

	// If the following setting is unset, 'istio-agent' will be using it
	// implicitly in certain code paths, despite saying that it defaults to
	// XDS address.
	CA_ADDR = newEnvVar("CA_ADDR", func(data *sidecarInjectData) (string, error) {
		if value := data.IstioValues.GetGlobal().GetCaAddress(); value != "" {
			return value, nil
		}
		return data.ProxyConfig.GetDiscoveryAddress(), nil
	})

	POD_NAME = newEnvVar("POD_NAME", func(data *sidecarInjectData) (string, error) {
		addressIdentifier := addressToPodNameAddition(data.Workload.Spec.Address)
		return data.Workload.Name + "-" + addressIdentifier, nil
	})

	POD_NAMESPACE = newEnvVar("POD_NAMESPACE", func(data *sidecarInjectData) (string, error) {
		return data.Workload.Namespace, nil
	})

	// Make sure that 'istio-agent' picks a given address as the primary address of this workload.
	INSTANCE_IP = newEnvVar("INSTANCE_IP", func(data *sidecarInjectData) (string, error) {
		return data.Workload.Spec.Address, nil
	})

	SERVICE_ACCOUNT = newEnvVar("SERVICE_ACCOUNT", func(data *sidecarInjectData) (string, error) {
		return data.Workload.Spec.ServiceAccount, nil
	})

	HOST_IP = newEnvVar("HOST_IP", func(data *sidecarInjectData) (string, error) {
		return data.Workload.Spec.Address, nil
	})

	CANONICAL_SERVICE = newEnvVar("CANONICAL_SERVICE", func(data *sidecarInjectData) (string, error) {
		return data.Workload.Labels["service.istio.io/canonical-name"], nil
	})

	CANONICAL_REVISION = newEnvVar("CANONICAL_REVISION", func(data *sidecarInjectData) (string, error) {
		return data.Workload.Labels["service.istio.io/canonical-revision"], nil
	})

	PROXY_CONFIG = newEnvVar("PROXY_CONFIG", func(data *sidecarInjectData) (string, error) {
		if data.ProxyConfig == nil {
			return "", nil
		}
		value, err := new(jsonpb.Marshaler).MarshalToString(data.ProxyConfig)
		if err != nil {
			return "", err
		}
		return string(value), nil
	})

	ISTIO_META_CLUSTER_ID = newEnvVar("ISTIO_META_CLUSTER_ID", func(data *sidecarInjectData) (string, error) {
		if name := data.IstioValues.GetGlobal().GetMultiCluster().GetClusterName(); name != "" {
			return name, nil
		}
		return "Kubernetes", nil
	})

	ISTIO_META_INTERCEPTION_MODE = newEnvVar("ISTIO_META_INTERCEPTION_MODE", func(data *sidecarInjectData) (string, error) {
		if mode := data.Workload.Annotations[annotation.SidecarInterceptionMode.Name]; mode != "" {
			return mode, nil
		}
		return data.ProxyConfig.GetInterceptionMode().String(), nil
	})

	ISTIO_META_NETWORK = newEnvVar("ISTIO_META_NETWORK", func(data *sidecarInjectData) (string, error) {
		return data.IstioValues.GetGlobal().GetNetwork(), nil
	})

	// Workload labels
	ISTIO_METAJSON_LABELS = newEnvVar("ISTIO_METAJSON_LABELS", func(data *sidecarInjectData) (string, error) {
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

	ISTIO_META_WORKLOAD_NAME = newEnvVar("ISTIO_META_WORKLOAD_NAME", func(data *sidecarInjectData) (string, error) {
		return data.GetAppOrServiceAccount(), nil
	})

	ISTIO_META_OWNER = newEnvVar("ISTIO_META_OWNER", func(data *sidecarInjectData) (string, error) {
		return fmt.Sprintf("kubernetes://apis/networking.istio.io/v1alpha3/namespaces/%s/workloadentries/%s", data.Workload.Namespace, data.Workload.Name), nil
	})

	ISTIO_META_MESH_ID = newEnvVar("ISTIO_META_MESH_ID", func(data *sidecarInjectData) (string, error) {
		if value := data.IstioValues.GetGlobal().GetMeshID(); value != "" {
			return value, nil
		}
		return data.IstioValues.GetGlobal().GetTrustDomain(), nil
	})

	SIDECAR_ENV = []envVar{
		JWT_POLICY,
		PILOT_CERT_PROVIDER,
		CA_ADDR,
		POD_NAME,
		POD_NAMESPACE,
		INSTANCE_IP,
		SERVICE_ACCOUNT,
		HOST_IP,
		CANONICAL_SERVICE,
		CANONICAL_REVISION,
		PROXY_CONFIG,
		ISTIO_META_CLUSTER_ID,
		ISTIO_META_INTERCEPTION_MODE,
		ISTIO_META_NETWORK,
		ISTIO_METAJSON_LABELS,
		ISTIO_META_WORKLOAD_NAME,
		ISTIO_META_OWNER,
		ISTIO_META_MESH_ID,
	}
)

func (d *sidecarInjectData) GetEnv() ([]string, error) {
	vars := make([]string, 0, len(d.ProxyConfig.GetProxyMetadata())+len(SIDECAR_ENV))
	// lower priority
	for name, value := range d.ProxyConfig.GetProxyMetadata() {
		vars = append(vars, fmt.Sprintf("%s=%s", name, value))
	}
	// higher priority
	for _, envar := range SIDECAR_ENV {
		value, err := envar.Value(d)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate a value of the environment variable %q: %w", envar.Name, err)
		}
		vars = append(vars, fmt.Sprintf("%s=%s", envar.Name, value))
	}
	return vars, nil
}

func (d *sidecarInjectData) GetEnvFile() ([]byte, error) {
	vars, err := d.GetEnv()
	if err != nil {
		return nil, err
	}
	return []byte(strings.Join(vars, "\n")), nil
}

func (d *sidecarInjectData) GetIstioProxyArgs() []string {
	return []string{
		"proxy",
		"sidecar",
		"--serviceCluster", // `istio-agent` will only respect this setting from command-line
		d.GetServiceCluster(),
		"--concurrency",
		fmt.Sprintf("%d", d.GetConcurrency()), // `istio-agent` will only respect this setting from command-line
		"--proxyLogLevel",
		d.GetLogLevel(),
		"--proxyComponentLogLevel",
		d.GetComponentLogLevel(),
		"--trust-domain",
		d.GetTrustDomain(),
	}
}

func (d *sidecarInjectData) GetIstioNamespace() string {
	return d.IstioNamespace
}

func (d *sidecarInjectData) GetCanonicalDiscoveryAddress() string {
	revision := d.IstioValues.GetGlobal().GetRevision()
	if revision != "" {
		revision = "-" + revision
	}
	return fmt.Sprintf("istiod%s.%s.svc:15012", revision, d.GetIstioNamespace())
}

func (d *sidecarInjectData) GetIstioProxyHosts() []string {
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
		d.IstioValues.GetGlobal().GetRemotePolicyAddress(),
		d.IstioValues.GetGlobal().GetRemotePilotAddress(),
		d.IstioValues.GetGlobal().GetRemoteTelemetryAddress(),
		d.IstioValues.GetGlobal().GetCaAddress(),
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
		segments := strings.SplitN(host, ".", 3)
		svc := segments[0]
		ns := d.Workload.Namespace
		if len(segments) > 1 {
			ns = segments[1]
		}
		hosts = append(hosts, getClusterLocalAliases(svc, ns)...)
	}
	return hosts
}

func (d *sidecarInjectData) GetIstioProxyContainerName() string {
	return fmt.Sprintf("%s-%s-istio-proxy", d.Workload.Namespace, d.Workload.Name)
}

func (d *sidecarInjectData) GetIstioProxyImage() string {
	if value := d.Workload.Annotations[annotation.SidecarProxyImage.Name]; value != "" {
		return value
	}
	return fmt.Sprintf("%s/%s:%s",
		d.IstioValues.GetGlobal().GetHub(),
		d.IstioValues.GetGlobal().GetProxy().GetImage(),
		d.IstioValues.GetGlobal().GetTag())
}

func (d *sidecarInjectData) GetAppOrServiceAccount() string {
	if value := d.Workload.Spec.Labels["app"]; value != "" {
		return value
	}
	if value := d.Workload.Labels["app"]; value != "" {
		return value
	}
	return d.Workload.Spec.ServiceAccount
}

func (d *sidecarInjectData) GetServiceCluster() string {
	return fmt.Sprintf("%s.%s", d.GetAppOrServiceAccount(), d.Workload.Namespace)
}

func (d *sidecarInjectData) GetConcurrency() int32 {
	return d.ProxyConfig.GetConcurrency().GetValue()
}

func (d *sidecarInjectData) GetTrustDomain() string {
	return d.IstioValues.GetGlobal().GetTrustDomain()
}

func (d *sidecarInjectData) GetLogLevel() string {
	if value := d.Workload.Annotations[annotation.SidecarLogLevel.Name]; value != "" {
		return value
	}
	if value := d.IstioValues.GetGlobal().GetProxy().GetLogLevel(); value != "" {
		return value
	}
	return "info"
}

func (d *sidecarInjectData) GetComponentLogLevel() string {
	if value := d.Workload.Annotations[annotation.SidecarComponentLogLevel.Name]; value != "" {
		return value
	}
	if value := d.IstioValues.GetGlobal().GetProxy().GetComponentLogLevel(); value != "" {
		return value
	}
	return "misc:info"
}

func isClusterLocal(host string) bool {
	segments := strings.Split(host, ".")
	switch len(segments) {
	case 1, 2:
		return true // TODO(yskopets): beware of fake positives like `docker.io`
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

func getClusterLocalAliases(svc, ns string) []string {
	base := svc + "." + ns
	return []string{
		base,
		base + ".svc",
		base + ".svc.cluster",
		base + ".svc.cluster.local",
	}
}
