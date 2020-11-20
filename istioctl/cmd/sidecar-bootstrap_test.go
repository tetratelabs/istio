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
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kube "k8s.io/client-go/kubernetes"
	kubeFake "k8s.io/client-go/kubernetes/fake"
	kubeTesting "k8s.io/client-go/testing"

	clientnetworking "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"

	networking "istio.io/api/networking/v1alpha3"
)

type vmBootstrapTestcase struct {
	args              []string
	cannedIstioConfig []clientnetworking.WorkloadEntry
	cannedK8sConfig   []runtime.Object
	expectedString    string
	shouldFail        bool
}

var (
	emptyIstioConfig = make([]clientnetworking.WorkloadEntry, 0)
	emptyK8sConfig   = make([]runtime.Object, 0)

	istioStaticWorkspace = []clientnetworking.WorkloadEntry{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "workload",
				Namespace: "NS",
			},
			Spec: networking.WorkloadEntry{
				Address:        "127.0.0.1",
				ServiceAccount: "test",
			},
		},
	}

	// see `samples/certs` in the root of the repo
	caCert = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----`

	baseTempdir, _ = ioutil.TempDir("", "vm_bootstrap_test_dir")

	fullK8sConfig = []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "istio-system",
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ca-root-cert",
				Namespace: "istio-system",
			},
			Data: map[string]string{
				"root-cert.pem": caCert,
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
				"values": `{
				  "global": {
					"jwtPolicy": "third-party-jwt",
					"pilotCertProvider": "istiod"
				  }
				}`,
			},
		},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: "istio-system",
			},
			Secrets: []corev1.ObjectReference{{
				Name: "default-token-6n2ql",
			}},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-token-6n2ql",
				Namespace: "istio-system",
			},
			Type: "kubernetes.io/service-account-token",
			Data: map[string][]byte{
				"ca.crt": []byte(base64.StdEncoding.EncodeToString([]byte(
					`-----BEGIN CERTIFICATE-----
MIICyDCCAbCgAwIBAgIBADANBgkqhkiG9w0BAQsFADAVMRMwEQYDVQQDEwprdWJl
cm5ldGVzMB4XDTIwMTAyMjE1NTM1MVoXDTMwMTAyMDE1NTM1MVowFTETMBEGA1UE
AxMKa3ViZXJuZXRlczCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALHj
8lB2a7cGe7DWdfUzVlxDYTN63UahoeJIOtqiMn4a9yXJJFUwedlRRiv+GDHVNJ4I
NSrhaH13dOGZWL6DRQRXBOPSEUZ/TJjXFPtS5y1Cxd6nAgPCa1I+eyFGU8e9pss9
/6uo7PyLx6zqcQDawtqZte90nJyYjsYuMTvRzMFbAAwBt1OTrNF2PboCEuuj3dTn
29ggev5mj8JOzHYOjWASAj/zqm706AwWv0y50IfNKR0j5t+fYI+1kj5Qfy09BuP5
kCHq9YT8LT4PddE05ztaTkSW2DSHIHt3aEdsvczD6VU633tDn5dQ64RCAZKnQAEB
1FZ3xHUppH9RuQyYyLkCAwEAAaMjMCEwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB
/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAAllw6xJeu1+tp2m8NgPJuxpBvAf
Xo//ySR69jlDQxmCIafR/0lWMkzPDQy8gv+INhpeOK99gooEU/qC9pjs1m53MzmQ
kM3Ru/GM1uqMnUmTuwLptxFJxDjSKcmXcYn63k1BdriExs3Wl2IiFDHjUdfGaUo0
twjnHmtommvAAbMeGyMSoM1Wd8mIzJO9Bk0d5l4wEZeA4corbAIDyoAhM3pyNrxL
yFkHk8ul+CzDoZSdfQulovH/T0GShzE6WFVO7LzOVQwylI1qYMoV20/8e4cO/L0K
ewSti8ZCFKFSQcfSMCjKDzPB5mnXiec9m+qtnv+cNS7nZ8EmGYMsffP1rx4=
-----END CERTIFICATE-----`))),
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
	}
)

func TestVmBootstrap(t *testing.T) {
	cases := []vmBootstrapTestcase{
		// No all flag, or no workload entry.
		{
			args:              strings.Split("x sidecar-bootstrap", " "),
			cannedIstioConfig: emptyIstioConfig,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "sidecar-bootstrap command requires either a <workload-entry-name>[.<namespace>] argument or the --all flag",
			shouldFail:        true,
		},
		// Workload Entry + all flag
		{
			args:              strings.Split("x sidecar-bootstrap --all workload.NS", " "),
			cannedIstioConfig: emptyIstioConfig,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "sidecar-bootstrap command requires either a <workload-entry-name>[.<namespace>] argument or the --all flag but not both",
			shouldFail:        true,
		},
		// unknown workload entry, okay to have fake dumpDir here.
		{
			args:              strings.Split("x sidecar-bootstrap workload.fakeNS --local-dir /tmp/", " "),
			cannedIstioConfig: istioStaticWorkspace,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString: `unable to find WorkloadEntry(s): failed to fetch WorkloadEntry ` +
				`kubernetes://apis/networking.istio.io/v1beta1/namespaces/fakeNS/workloadentries/workload: ` +
				`workloadentries.networking.istio.io "workload" not found`,
			shouldFail: true,
		},
		// known workload entry, known secret
		{
			args:              strings.Split("x sidecar-bootstrap workload.NS --local-dir "+path.Join(baseTempdir, "derived_output"), " "),
			cannedIstioConfig: istioStaticWorkspace,
			cannedK8sConfig:   fullK8sConfig,
			expectedString:    "",
			shouldFail:        false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyVMCommandCaseOutput(t, c)
		})
	}
}

func verifyVMCommandCaseOutput(t *testing.T, c vmBootstrapTestcase) {
	t.Helper()

	backupInterfaceFactory := interfaceFactory
	defer func() {
		interfaceFactory = backupInterfaceFactory
	}()

	configStoreFactory = mockClientFactoryGenerator(func(client istioclient.Interface) {
		for _, cfg := range c.cannedIstioConfig {
			_, err := client.NetworkingV1alpha3().WorkloadEntries(cfg.Namespace).Create(context.TODO(), &cfg, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	interfaceFactory = FakeKubeInterfaceGeneratorFunc(mockInterfaceFactoryGenerator(c.cannedK8sConfig)).
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

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.shouldFail {
		if fErr == nil {
			t.Fatalf("Command should have failed for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Command should not have failed for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
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
