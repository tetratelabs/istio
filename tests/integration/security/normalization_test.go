// +build integ
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

package security

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/test/framework/resource"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/tests/integration/security/util"
)

var (
	retryTimeout = time.Second * 30
	retryDelay   = time.Millisecond * 100
)

func TestNormalization(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.routing").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "test-ns1",
				Inject: true,
			})
			var a, b, naked echo.Instance
			echoboot.NewBuilderOrFail(t, t).
				With(&a, util.EchoConfig("a", ns, false, nil)).
				With(&b, util.EchoConfig("b", ns, false, nil)).
				With(&naked, util.EchoConfig("naked", ns, false, echo.NewAnnotations().
					SetBool(echo.SidecarInject, false))).
				BuildOrFail(t)

			type expect struct {
				in, out string
			}
			cases := []struct {
				ntype        meshconfig.MeshConfig_ProxyPathNormalization_NormalizationType
				expectations []expect
			}{
				{
					meshconfig.MeshConfig_ProxyPathNormalization_NONE,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/app/../admin"},
						{"/app", "/app"},
						{"/app//", "/app//"},
						{"/app/%2f", "/app/%2f"},
						{"/app%2f/", "/app%2f/"},
						{"/xyz%30..//abc", "/xyz%30..//abc"},
						{"/app/%2E./admin", "/app/%2E./admin"},
						{`/app\admin`, `/app\admin`},
						{`/app/\/\/\admin`, `/app/\/\/\admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/%2Fapp%5cadmin%5Cabc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`},
						{`/app//../admin`, `/app//../admin`},
						{`/app//../../admin`, `/app//../../admin`},
					},
				},
				{
					meshconfig.MeshConfig_ProxyPathNormalization_BASE,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/admin"},
						{"/app", "/app"},
						{"/app//", "/app//"},
						{"/app/%2f", "/app/%2f"},
						{"/app%2f/", "/app%2f/"},
						{"/xyz%30..//abc", "/xyz0..//abc"},
						{"/app/%2E./admin", "/admin"},
						{`/app\admin`, `/app/admin`},
						{`/app/\/\/\admin`, `/app//////admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/%2Fapp%5cadmin%5Cabc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/%5Capp%2f%5c%2F..%2fadmin%5c/abc`},
						{`/app//../admin`, `/app/admin`},
						{`/app//../../admin`, `/admin`},
					},
				},
				{
					meshconfig.MeshConfig_ProxyPathNormalization_MERGE_SLASHES,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/admin"},
						{"/app", "/app"},
						{"/app//", "/app/"},
						{"/app/%2f", "/app/%2f"},
						{"/app%2f/", "/app%2f/"},
						{"/xyz%30..//abc", "/xyz0../abc"},
						{"/app/%2E./admin", "/admin"},
						{`/app\admin`, `/app/admin`},
						{`/app/\/\/\admin`, `/app/admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/%2Fapp%5cadmin%5Cabc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/%5Capp%2f%5c%2F..%2fadmin%5c/abc`},
						{`/app//../admin`, `/app/admin`},
						{`/app//../../admin`, `/admin`},
					},
				},
				{
					meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/admin"},
						{"/app", "/app"},
						{"/app//", "/app/"},
						{"/app/%2f", "/app/"},
						{"/app%2f/", "/app/"},
						{"/xyz%30..//abc", "/xyz0../abc"},
						{"/app/%2E./admin", "/admin"},
						{`/app\admin`, `/app/admin`},
						{`/app/\/\/\admin`, `/app/admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/app/admin/abc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/app/admin/abc`},
						{`/app//../admin`, `/app/admin`},
						{`/app//../../admin`, `/admin`},
					},
				},
			}
			for _, tt := range cases {
				t.NewSubTest(tt.ntype.String()).Run(func(t framework.TestContext) {
					PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), fmt.Sprintf(`
pathNormalization:
  normalization: %v`, tt.ntype.String()))
					for _, c := range []echo.Instance{a} {
						for _, dst := range []echo.Instance{b, naked} {
							for _, tt := range tt.expectations {
								t.NewSubTest(fmt.Sprintf("%s/%s", dst.Config().Service, tt.in)).Run(func(t framework.TestContext) {
									retry.UntilSuccessOrFail(t, func() (err error) {
										responses, err := c.Call(echo.CallOptions{
											Target:   dst,
											Path:     tt.in,
											PortName: "http",
										})
										if err == nil {
											err = responses.CheckKey("URL", tt.out)
										}
										if err != nil {
											return fmt.Errorf("%s to %s:%s using %s: expected success but failed: %v",
												c.Config().Service, dst.Config().Service, "http", scheme.HTTP, err)
										}
										return nil
									}, retry.Timeout(retryTimeout), retry.Delay(retryDelay))
								})
							}
						}
					}
				})
			}
		})
}

func PatchMeshConfig(t framework.TestContext, ns string, clusters resource.Clusters, patch string) {
	errG := multierror.Group{}
	origCfg := map[string]string{}
	mu := sync.RWMutex{}

	cmName := "istio"
	for _, c := range clusters {
		c := c
		errG.Go(func() error {
			cm, err := c.CoreV1().ConfigMaps(ns).Get(context.TODO(), cmName, v1.GetOptions{})
			if err != nil {
				return err
			}
			mcYaml, ok := cm.Data["mesh"]
			if !ok {
				return fmt.Errorf("mesh config was missing in istio config map for %s", c.Name())
			}
			mu.Lock()
			origCfg[c.Name()] = cm.Data["mesh"]
			mu.Unlock()
			mc := &meshconfig.MeshConfig{}
			if err := gogoprotomarshal.ApplyYAML(mcYaml, mc); err != nil {
				return err
			}
			if err := gogoprotomarshal.ApplyYAML(patch, mc); err != nil {
				return err
			}
			cm.Data["mesh"], err = gogoprotomarshal.ToYAML(mc)
			if err != nil {
				return err
			}
			_, err = c.CoreV1().ConfigMaps(ns).Update(context.TODO(), cm, v1.UpdateOptions{})
			if err != nil {
				return err
			}
			scopes.Framework.Infof("patched %s meshconfig:\n%s", c.Name(), cm.Data["mesh"])
			pl, err := c.CoreV1().Pods(ns).List(context.TODO(), v1.ListOptions{LabelSelector: "app=istiod"})
			if err != nil {
				return err
			}
			for _, pod := range pl.Items {
				patchBytes := fmt.Sprintf(`{ "metadata": {"annotations": { "test.istio.io/mesh-config-hash": "%s" } } }`, hash(cm.Data["mesh"]))
				// Trigger immediate kubelet resync, to avoid 1 min+ delay on update
				// https://github.com/kubernetes/kubernetes/issues/30189
				_, err := c.CoreV1().Pods(ns).Patch(context.TODO(), pod.Name,
					types.MergePatchType, []byte(patchBytes), v1.PatchOptions{FieldManager: "istio-ci"})
				if err != nil {
					return fmt.Errorf("patch %v: %v", patchBytes, err)
				}
			}
			return nil
		})
	}
	t.WhenDone(func() error {
		errG := multierror.Group{}
		mu.RLock()
		defer mu.RUnlock()
		for cn, mcYaml := range origCfg {
			cn, mcYaml := cn, mcYaml
			c := clusters.GetByName(cn)
			errG.Go(func() error {
				cm, err := c.CoreV1().ConfigMaps(ns).Get(context.TODO(), cmName, v1.GetOptions{})
				if err != nil {
					return err
				}
				cm.Data["mesh"] = mcYaml
				_, err = c.CoreV1().ConfigMaps(ns).Update(context.TODO(), cm, v1.UpdateOptions{})
				return err
			})
		}
		if err := errG.Wait().ErrorOrNil(); err != nil {
			return fmt.Errorf("failed cleaning up cluster-local config: %v", err)
		}
		return nil
	})
	if err := errG.Wait().ErrorOrNil(); err != nil {
		t.Fatal(err)
	}
}

func hash(s string) string {
	h := md5.New()
	_, _ = io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
}
