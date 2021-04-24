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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mholt/archiver"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kube "k8s.io/client-go/kubernetes"
	kubeFake "k8s.io/client-go/kubernetes/fake"
	kubeTesting "k8s.io/client-go/testing"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istioclientFake "istio.io/client-go/pkg/clientset/versioned/fake"
	"istio.io/istio/operator/pkg/object"
)

var (
	k8sScheme = runtime.NewScheme()
)

func init() {
	metav1.AddToGroupVersion(k8sScheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(kubeFake.AddToScheme(k8sScheme))
	utilruntime.Must(istioclientFake.AddToScheme(k8sScheme))
}

func parseK8sObjects(data []byte) ([]runtime.Object, error) {
	objects, err := object.ParseK8sObjectsFromYAMLManifest(string(data))
	if err != nil {
		return nil, err
	}
	out := make([]runtime.Object, len(objects))
	for i, obj := range objects {
		o, err := k8sScheme.New(obj.GroupVersionKind())
		if err != nil {
			return nil, err
		}
		raw, err := obj.JSON()
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(raw, o)
		if err != nil {
			return nil, err
		}
		out[i] = o
	}
	return out, nil
}

func parseK8sObjectsFromFile(path string) []runtime.Object {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(fmt.Errorf("failed to read k8s objects from file %q: %w", path, err))
	}
	objs, err := parseK8sObjects(data)
	if err != nil {
		panic(fmt.Errorf("failed to parse k8s objects from file %q: %w", path, err))
	}
	return objs
}

func listFilesRecursively(dir string) (expectedFiles []string, err error) {
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			expectedFiles = append(expectedFiles, relPath)
		}
		return nil
	})
	return
}

func verifyBootstrapOutputDir(t *testing.T, expectedDir string, actualDir string) {
	g := NewGomegaWithT(t)

	expectedFiles, err := listFilesRecursively(expectedDir)
	g.Expect(err).NotTo(HaveOccurred())

	actualFiles, err := listFilesRecursively(actualDir)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(actualFiles).To(ConsistOf(expectedFiles))

	for _, file := range expectedFiles {
		expected, err := ioutil.ReadFile(filepath.Join(expectedDir, file))
		g.Expect(err).NotTo(HaveOccurred())

		actual, err := ioutil.ReadFile(filepath.Join(actualDir, file))
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(string(actual)).To(Equal(string(expected)),
			fmt.Sprintf(`contents of %q doesn't match:

actual:
>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
%s
<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<

expected:
>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>
%s
<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<
`, file, string(actual), string(expected)))
	}
}

func verifyBootstrapOutputArchive(t *testing.T, expectedDir string, actualFile string) {
	g := NewGomegaWithT(t)

	tempdir, err := ioutil.TempDir("", "")
	g.Expect(err).NotTo(HaveOccurred())

	defer os.RemoveAll(tempdir)

	targz := archiver.TarGz{Tar: &archiver.Tar{OverwriteExisting: true}}
	err = targz.Unarchive(actualFile, tempdir)
	g.Expect(err).NotTo(HaveOccurred())

	verifyBootstrapOutputDir(t, expectedDir, tempdir)
}

func expectedNextStepsForBootstrapDir(outputDir string) string {
	return `Generated files have been saved to the directory ` + fmt.Sprintf("`%s`", outputDir) + `

Next steps:

  1. Copy the contents of ` + fmt.Sprintf("`%s`", outputDir) + ` directory to the remote host represented by the WorkloadEntry

  2. Once on the remote host, run ` + "`<dir>/bin/start-istio-proxy.sh`" + ` to start Istio Proxy in a Docker container
`
}

func expectedNextStepsForBootstrapArchive(outputFile string) string {
	return `Generated files have been saved into the TGZ archive ` + fmt.Sprintf("`%s`", outputFile) + `

Next steps:

  1. Copy the file ` + fmt.Sprintf("`%s`", outputFile) + ` to the remote host represented by the WorkloadEntry

  2. Once on the remote host,

     1. run ` + fmt.Sprintf("`tar -xvf %s`", filepath.Base(outputFile)) + ` to extract archive into the working directory

     2. run ` + "`./bin/start-istio-proxy.sh`" + ` to start Istio Proxy in a Docker container
`
}

type vmBootstrapTestcase struct {
	now            time.Time
	cwd            string
	args           []string
	istioResources []runtime.Object
	k8sResources   []runtime.Object
	shouldFail     bool
	expectedStdout string
	expectedStderr string
	verifyFunc     func(t *testing.T)
}

func TestVmBootstrap(t *testing.T) {
	g := NewGomegaWithT(t)

	baseTempdir, err := ioutil.TempDir("", "vm_bootstrap_test_dir")
	g.Expect(err).NotTo(HaveOccurred())
	baseTempdir, err = filepath.EvalSymlinks(baseTempdir)
	g.Expect(err).NotTo(HaveOccurred())

	defer os.RemoveAll(baseTempdir)

	cases := []vmBootstrapTestcase{
		{
			args:           []string{"x", "sidecar-bootstrap"},
			shouldFail:     true,
			expectedStdout: "Error: sidecar-bootstrap command requires either a <workload-entry-name>[.<namespace>] argument or the --all flag\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "--all", "vm.ns"},
			shouldFail:     true,
			expectedStdout: "Error: sidecar-bootstrap command requires either a <workload-entry-name>[.<namespace>] argument or the --all flag but not both\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "vm.ns", "-o", "--output-file", "path/to/file"},
			shouldFail:     true,
			expectedStdout: "Error: use either -o or --output-file but not both\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "vm.ns", "-o", "--output-dir", "path/to/dir"},
			shouldFail:     true,
			expectedStdout: "Error: use either -o or --output-dir but not both\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "vm.ns", "--output-file", "path/to/file", "--output-dir", "path/to/dir"},
			shouldFail:     true,
			expectedStdout: "Error: use either --output-file or --output-dir but not both\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "vm.ns", "-o", "--dry-run"},
			shouldFail:     true,
			expectedStdout: "Error: it is not possible to use --dry-run flag together with -o\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "vm.ns", "--output-file", "path/to/file", "--dry-run"},
			shouldFail:     true,
			expectedStdout: "Error: it is not possible to use --dry-run flag together with --output-file\n",
		},
		{
			args:           []string{"x", "sidecar-bootstrap", "vm.ns", "--output-dir", "path/to/dir", "--dry-run"},
			shouldFail:     true,
			expectedStdout: "Error: it is not possible to use --dry-run flag together with --output-dir\n",
		},
		// unknown workload entry, okay to have fake dumpDir here.
		{
			args:       []string{"x", "sidecar-bootstrap", "vm.non-existing", "--output-dir", "path/to/dir"},
			shouldFail: true,
			expectedStdout: `Error: unable to find WorkloadEntry(s): failed to fetch WorkloadEntry ` +
				`kubernetes://apis/networking.istio.io/v1beta1/namespaces/non-existing/workloadentries/vm: ` +
				`workloadentries.networking.istio.io "vm" not found` + "\n",
		},
		// save generated files into a dir
		func() vmBootstrapTestcase {
			outputDir, err := ioutil.TempDir(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			return vmBootstrapTestcase{
				args:           []string{"x", "sidecar-bootstrap", "my-vm.my-ns", "--output-dir", outputDir},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapDir(outputDir),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputDir(t, "testdata/sidecar-bootstrap/basic/output/single", outputDir)
				},
			}
		}(),
		// save generated files into a TGZ file
		func() vmBootstrapTestcase {
			outputFile, err := ioutil.TempFile(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			defer outputFile.Close()
			return vmBootstrapTestcase{
				args:           []string{"x", "sidecar-bootstrap", "my-vm.my-ns", "--output-file", outputFile.Name()},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapArchive(outputFile.Name()),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputArchive(t, "testdata/sidecar-bootstrap/basic/output/single", outputFile.Name())
				},
			}
		}(),
		// save generated files into a TGZ file with auto name
		func() vmBootstrapTestcase {
			outputDir, err := ioutil.TempDir(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			outputFile := filepath.Join(outputDir, "my-vm.my-ns.20201016195037.tgz")
			return vmBootstrapTestcase{
				now:            time.Date(2020, time.October, 16, 19, 50, 37, 123, time.UTC),
				cwd:            outputDir,
				args:           []string{"x", "sidecar-bootstrap", "my-vm.my-ns", "-o"},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapArchive(outputFile),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputArchive(t, "testdata/sidecar-bootstrap/basic/output/single", outputFile)
				},
			}
		}(),
		// save generated files into a dir (all WorkloadEntrys)
		func() vmBootstrapTestcase {
			outputDir, err := ioutil.TempDir(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			return vmBootstrapTestcase{
				args:           []string{"x", "sidecar-bootstrap", "-a", "-n", "my-ns", "--output-dir", outputDir},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapDir(outputDir),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputDir(t, "testdata/sidecar-bootstrap/basic/output/multi", outputDir)
				},
			}
		}(),
		// save generated files into a TGZ file (all WorkloadEntrys)
		func() vmBootstrapTestcase {
			outputFile, err := ioutil.TempFile(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			defer outputFile.Close()
			return vmBootstrapTestcase{
				args:           []string{"x", "sidecar-bootstrap", "-a", "-n", "my-ns", "--output-file", outputFile.Name()},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapArchive(outputFile.Name()),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputArchive(t, "testdata/sidecar-bootstrap/basic/output/multi", outputFile.Name())
				},
			}
		}(),
		// save generated files into a TGZ file with auto name (all WorkloadEntrys)
		func() vmBootstrapTestcase {
			outputDir, err := ioutil.TempDir(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			outputFile := filepath.Join(outputDir, "my-ns.20201016195037.tgz")
			return vmBootstrapTestcase{
				now:            time.Date(2020, time.October, 16, 19, 50, 37, 123, time.UTC),
				cwd:            outputDir,
				args:           []string{"x", "sidecar-bootstrap", "-a", "-n", "my-ns", "-o"},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/basic/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapArchive(outputFile),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputArchive(t, "testdata/sidecar-bootstrap/basic/output/multi", outputFile)
				},
			}
		}(),
		// save generated files into a dir (GKE-like config)
		func() vmBootstrapTestcase {
			outputDir, err := ioutil.TempDir(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			return vmBootstrapTestcase{
				args:           []string{"x", "sidecar-bootstrap", "my-vm.my-ns", "--output-dir", outputDir},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/gke/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/gke/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapDir(outputDir),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputDir(t, "testdata/sidecar-bootstrap/gke/output/single", outputDir)
				},
			}
		}(),
		// save generated files into a dir (EKS-like config)
		func() vmBootstrapTestcase {
			outputDir, err := ioutil.TempDir(baseTempdir, "")
			g.Expect(err).NotTo(HaveOccurred())
			return vmBootstrapTestcase{
				args:           []string{"x", "sidecar-bootstrap", "my-vm.my-ns", "--output-dir", outputDir},
				istioResources: parseK8sObjectsFromFile("testdata/sidecar-bootstrap/eks/input/istio.yaml"),
				k8sResources:   parseK8sObjectsFromFile("testdata/sidecar-bootstrap/eks/input/k8s.yaml"),
				shouldFail:     false,
				expectedStdout: "",
				expectedStderr: expectedNextStepsForBootstrapDir(outputDir),
				verifyFunc: func(t *testing.T) {
					verifyBootstrapOutputDir(t, "testdata/sidecar-bootstrap/eks/output/single", outputDir)
				},
			}
		}(),
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d: %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyVMCommandCaseOutput(t, c)
		})
	}
}

func mockConfigStoreFactoryGenerator(objects ...runtime.Object) func() (istioclient.Interface, error) {
	return func() (istioclient.Interface, error) {
		client := istioclientFake.NewSimpleClientset(objects...)
		return client, nil
	}
}

func verifyVMCommandCaseOutput(t *testing.T, c vmBootstrapTestcase) {
	t.Helper()

	g := NewGomegaWithT(t)

	backupNow := now
	defer func() {
		now = backupNow
	}()

	backupInterfaceFactory := interfaceFactory
	defer func() {
		interfaceFactory = backupInterfaceFactory
	}()

	backupConfigStoreFactory := configStoreFactory
	defer func() {
		configStoreFactory = backupConfigStoreFactory
	}()

	backupVMBootstrapCmd := vmBootstrapCmd
	defer func() {
		vmBootstrapCmd = backupVMBootstrapCmd
	}()

	now = func() time.Time {
		return c.now
	}

	vmBootstrapCmd = func() *cobra.Command {
		return NewVMBootstrapCommand(VMBootstrapCommandOpts{
			GlobalProxyConfigOverridesConfigMap: "global-mesh-expansion",
		})
	}

	// setup fake k8s client
	interfaceFactory = FakeKubeInterfaceGeneratorFunc(mockInterfaceFactoryGenerator(c.k8sResources)).
		Configure(func(clientset *kubeFake.Clientset) {
			clientset.PrependReactor("create", "serviceaccounts", func(action kubeTesting.Action) (handled bool, ret runtime.Object, err error) {
				if action.GetSubresource() == "token" {
					createAction := action.(kubeTesting.CreateAction)
					tokenRequest := createAction.GetObject().(*authenticationv1.TokenRequest)
					tokenRequest.Status.Token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c" // nolint: lll
					tokenRequest.Status.ExpirationTimestamp = metav1.Date(2020, time.October, 16, 19, 50, 37, 123, time.UTC)
					return true, tokenRequest, nil
				}
				return false, nil, nil
			})
		})

	// setup fake Istio client
	configStoreFactory = mockConfigStoreFactoryGenerator(c.istioResources...)

	rootCmd := GetRootCmd(c.args)

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	err := func() error {
		cwd := func() string {
			cwd, err := os.Getwd()
			g.Expect(err).NotTo(HaveOccurred())
			abs, err := filepath.EvalSymlinks(cwd)
			g.Expect(err).NotTo(HaveOccurred())
			return abs
		}()

		defer func() {
			err := os.Chdir(cwd)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		if c.cwd != "" {
			err := os.Chdir(c.cwd)
			g.Expect(err).NotTo(HaveOccurred())
		}
		return rootCmd.Execute()
	}()

	if c.shouldFail {
		g.Expect(err).To(HaveOccurred())
	} else {
		g.Expect(err).NotTo(HaveOccurred())
	}

	g.Expect(stdout.String()).To(Equal(c.expectedStdout))
	g.Expect(stderr.String()).To(Equal(c.expectedStderr))

	if c.verifyFunc != nil {
		c.verifyFunc(t)
	}
}

type FakeKubeInterfaceGeneratorFunc func(kubeconfig string) (kube.Interface, error)

func (f FakeKubeInterfaceGeneratorFunc) Configure(fn func(clientset *kubeFake.Clientset)) FakeKubeInterfaceGeneratorFunc {
	return func(kubeconfig string) (kube.Interface, error) {
		clientset, err := f(kubeconfig)
		if err != nil {
			return nil, err
		}
		fn(clientset.(*kubeFake.Clientset))
		return clientset, nil
	}
}

func TestVmBundleCreate(t *testing.T) {
	var bundle BootstrapBundle

	testfunc := func(t *testing.T, remoteDir string) {
		items := processBundle(bundle, remoteDir)

		// Verify that all files should go remote directory
		for _, file := range items.filesToCopy {
			if file.dir != remoteDir {
				t.Fatal("Destination directory in bundle is not set properly")
			}
		}

		// Verify that docker run command contains proper mapping for files.
		filesToTest := []string{"istio-ca.pem", "istio-token", "sidecar.env"}
		for _, execCmd := range items.cmdsToExec {
			if strings.Contains(execCmd.cmd, "docker run") {
				// check all files.
				for _, testFile := range filesToTest {
					if !strings.Contains(execCmd.cmd, remoteDir+"/"+testFile) {
						t.Fatalf("docker run command for file %s is not formatted properly: %s", testFile, execCmd.cmd)
					}
				}
			}
		}
	}

	// Now test with real directory
	testfunc(t, "/var/sshtest/dir")

	// and with shell env variable.
	testfunc(t, "$TEST_VM_DIR")

}

func TestVmBootstrap_ProcessBundle(t *testing.T) {
	testCases := []struct {
		name          string
		bundle        BootstrapBundle
		expectedItems bootstrapItems
	}{
		{
			name: "w/ host names",
			bundle: BootstrapBundle{
				/* mesh */
				IstioCaCert:                []byte(`Istio CA certs`),
				IstioIngressGatewayAddress: "1.2.3.4",
				/* workload */
				ServiceAccountToken: []byte(`k8s SA token`),
				/* sidecar */
				IstioProxyContainerName: "istio-proxy",
				IstioProxyImage:         "proxyv2:latest",
				IstioProxyArgs:          []string{"proxy", "sidecar"},
				IstioProxyEnvironment:   []byte(`KEY=VALUE`),
				IstioProxyHosts:         []string{"istiod.istio-system.svc", "zipkin.istio-monitoring.svc"},
			},
			expectedItems: bootstrapItems{
				filesToCopy: []fileToCopy{
					{
						name: "sidecar.env",
						dir:  "/etc/istio-proxy",
						perm: os.FileMode(0644),
						data: []byte("KEY=VALUE"),
					},
					{
						name: "istio-ca.pem",
						dir:  "/etc/istio-proxy",
						perm: os.FileMode(0644),
						data: []byte("Istio CA certs"),
					},
					{
						name: "istio-token",
						dir:  "/etc/istio-proxy",
						perm: os.FileMode(0640),
						data: []byte("k8s SA token"),
					},
				},
				cmdsToExec: []cmdToExec{
					{
						cmd:      "docker rm --force istio-proxy",
						required: false,
					},
					{
						cmd: "docker run -d --name istio-proxy --restart unless-stopped --network host " +
							"-v /etc/istio-proxy/istio-ca.pem:/var/run/secrets/istio/root-cert.pem " +
							"-v /etc/istio-proxy/istio-token:/var/run/secrets/tokens/istio-token " +
							"--env-file /etc/istio-proxy/sidecar.env " +
							"--add-host istiod.istio-system.svc:1.2.3.4 " +
							"--add-host zipkin.istio-monitoring.svc:1.2.3.4 " +
							"proxyv2:latest " +
							"proxy sidecar",
						required: true,
					},
				},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			items := processBundle(testCase.bundle, "/etc/istio-proxy")

			g.Expect(items).To(Equal(testCase.expectedItems))
		})
	}
}

func TestVmBootstrap_IstioIngressGatewayAddress(t *testing.T) {
	testCases := []struct {
		name            string
		k8sConfig       []runtime.Object
		expectedAddress string
	}{
		{
			name: "minial mesh expansion configuration (just `values.meshExpansion.enabled` flag)",
			k8sConfig: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "istio-system",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio",
						Namespace: "istio-system",
					},
					Data: map[string]string{
						"mesh":         "",
						"meshNetworks": "networks: {}",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio-sidecar-injector",
						Namespace: "istio-system",
					},
					Data: map[string]string{
						"values": `{}`,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio-ingressgateway",
						Namespace: "istio-system",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{Name: "tcp-istiod", Port: 15012},
							{Name: "tls", Port: 15443},
						},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{Hostname: "x.y.z"},
								{IP: "1.2.3.4"},
							},
						},
					},
				},
			},
			expectedAddress: "x.y.z",
		},
		{
			name: "mesh expansion configuration with an alternative network Gateway Service",
			k8sConfig: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "istio-system",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio",
						Namespace: "istio-system",
					},
					Data: map[string]string{
						"mesh": "",
						"meshNetworks": `
                          networks:
                            "":
                              endpoints:
                              - fromRegistry: example
                              gateways:
                              - port: 15443
                                registryServiceName: vmgateway.istio-system.svc.cluster.local
`,
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio-sidecar-injector",
						Namespace: "istio-system",
					},
					Data: map[string]string{
						"values": `{}`,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmgateway",
						Namespace: "istio-system",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{Name: "tcp-istiod", Port: 15012},
							{Name: "tls", Port: 15443},
						},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{IP: "1.2.3.4"},
							},
						},
					},
				},
			},
			expectedAddress: "1.2.3.4",
		},
		{
			name: "mesh expansion configuration with a custom network gateway",
			k8sConfig: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "istio-system",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio",
						Namespace: "istio-system",
					},
					Data: map[string]string{
						"mesh": "",
						"meshNetworks": `
                          networks:
                            "vpc1":
                              gateways:
                              - port: 15443
                                address: 1.2.3.4
                            "irrelevant":
                              endpoints:
                              - fromRegistry: example
                              gateways:
                              - port: 15443
                                registryServiceName: vmgateway.istio-system.svc.cluster.local
`,
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istio-sidecar-injector",
						Namespace: "istio-system",
					},
					Data: map[string]string{
						"values": `{
                          "global": {
                            "network": "vpc1"
                          }
                        }`,
					},
				},
			},
			expectedAddress: "1.2.3.4",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			backupInterfaceFactory := interfaceFactory
			backupIstioNamespace := istioNamespace
			backupMeshConfigMapName := meshConfigMapName
			backupInjectConfigMapName := injectConfigMapName
			defer func() {
				interfaceFactory = backupInterfaceFactory
				istioNamespace = backupIstioNamespace
				meshConfigMapName = backupMeshConfigMapName
				injectConfigMapName = backupInjectConfigMapName
			}()

			interfaceFactory = mockInterfaceFactoryGenerator(testCase.k8sConfig)
			istioNamespace = "istio-system"
			meshConfigMapName = defaultMeshConfigMapName
			injectConfigMapName = defaultInjectConfigMapName

			meshConfig, err := getMeshConfigFromConfigMap("", "")
			g.Expect(err).NotTo(HaveOccurred())

			meshNetworksConfig, err := getMeshNetworksFromConfigMap("", "")
			g.Expect(err).NotTo(HaveOccurred())

			istioConfigValues, err := getConfigValuesFromConfigMap("")
			g.Expect(err).NotTo(HaveOccurred())

			kubeClient, err := interfaceFactory("")
			g.Expect(err).NotTo(HaveOccurred())

			address, err := getIstioIngressGatewayAddress(kubeClient, istioNamespace, meshConfig, meshNetworksConfig, istioConfigValues)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(address).To(Equal(testCase.expectedAddress))
		})
	}
}
