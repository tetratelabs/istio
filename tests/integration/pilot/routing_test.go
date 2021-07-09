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

package pilot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/tests/integration/pilot/common"
)

type TrafficTestCase struct {
	name      string
	config    string
	call      func() (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses) error

	// Multiple calls. Cannot be used with call/opts
	children []TrafficCall
}

type TrafficCall struct {
	name      string
	opts      echo.CallOptions
	call      func(options echo.CallOptions) (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses, error) error
}

func virtualServiceCases() []TrafficTestCase {
	cases := []TrafficTestCase{
		{
			name: "added header",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - b
  http:
  - route:
    - destination:
        host: b
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`,
			call: func() (echoclient.ParsedResponses, error) {
				return a.Call(echo.CallOptions{Target: b, PortName: "http"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				if response[0].RawResponse["Istio-Custom-Header"] != "user-defined-value" {
					return fmt.Errorf("missing request header, have %+v", response[0].RawResponse)
				}
				return nil
			},
		},
		{
			name: "redirect",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - b
  http:
  - match:
    - uri:
        exact: /
    redirect:
      uri: /new/path
  - match:
    - uri:
        exact: /new/path
    route:
    - destination:
        host: b`,
			call: func() (echoclient.ParsedResponses, error) {
				return a.Call(echo.CallOptions{Target: b, PortName: "http"})
			},
			validator: func(response echoclient.ParsedResponses) error {
				if response[0].URL != "/new/path" {
					return fmt.Errorf("incorrect URL, have %+v %+v", response[0].RawResponse["URL"], response[0].URL)
				}
				return nil
			},
		},
	}

	for _, split := range []int{50, 80} {
		split := split
		cases = append(cases, TrafficTestCase{
			name: fmt.Sprintf("shifting-%d", split),
			config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - b
  http:
  - route:
    - destination:
        host: b
      weight: %d
    - destination:
        host: naked
      weight: %d
`, split, 100-split),
			call: func() (echoclient.ParsedResponses, error) {
				return a.Call(echo.CallOptions{Target: b, PortName: "http", Count: 100})
			},
			validator: func(responses echoclient.ParsedResponses) error {
				if err := responses.CheckOK(); err != nil {
					return err
				}
				hitCount := map[string]int{}
				errorThreshold := 10
				for _, r := range responses {
					for _, h := range []string{"b", "naked"} {
						if strings.HasPrefix(r.Hostname, h+"-") {
							hitCount[h]++
							break
						}
					}
				}
				if !almostEquals(hitCount["b"], split, errorThreshold) {
					return fmt.Errorf("expected %v calls to b, got %v", split, hitCount["b"])
				}
				if !almostEquals(hitCount["naked"], 100-split, errorThreshold) {
					return fmt.Errorf("expected %v calls to naked, got %v", 100-split, hitCount["naked"])
				}
				return nil
			},
		})
	}
	return cases
}

func protocolSniffingCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, client := range []echo.Instance{a, naked} {
		for _, call := range []struct {
			// The port we call
			port string
			// The actual type of traffic we send to the port
			scheme scheme.Instance
		}{
			{"http", scheme.HTTP},
			{"auto-http", scheme.HTTP},
			{"tcp", scheme.TCP},
			{"auto-tcp", scheme.TCP},
			{"grpc", scheme.GRPC},
			{"auto-grpc", scheme.GRPC},
		} {
			cases = append(cases, TrafficTestCase{
				name: fmt.Sprintf("sniffing %v", call.port),
				call: func() (echoclient.ParsedResponses, error) {
					return client.Call(echo.CallOptions{Target: b, PortName: call.port, Scheme: call.scheme})
				},
				validator: func(responses echoclient.ParsedResponses) error {
					return responses.CheckOK()
				},
			})
		}
	}
	return cases
}

// trafficLoopCases contains tests to ensure traffic does not loop through the sidecar
func trafficLoopCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	for _, port := range []string{"15001", "15006"} {
		cases = append(cases, TrafficTestCase{
			name: port,
			call: func() (echoclient.ParsedResponses, error) {
				dwl, err := b.Workloads()
				if err != nil {
					return nil, err
				}
				cwl, err := a.Workloads()
				if err != nil {
					return nil, err
				}
				resp, err := cwl[0].ForwardEcho(context.Background(), &epb.ForwardEchoRequest{
					Url:   fmt.Sprintf("http://%s:%s", dwl[0].Address(), port),
					Count: 1,
				})
				// Ideally we would actually check to make sure we do not blow up the pod,
				// but I couldn't find a way to reliably detect this.
				if err == nil {
					return nil, fmt.Errorf("expected request to fail, but it didn't: %v", resp)
				}
				return nil, nil
			},
			validator: func(responses echoclient.ParsedResponses) error {
				// We only care if there was an error
				return nil
			},
		})
	}
	return cases
}

// autoPassthroughCases tests that we cannot hit unexpected destinations when using AUTO_PASSTHROUGH
func autoPassthroughCases() []TrafficTestCase {
	cases := []TrafficTestCase{}
	// We test the cross product of all Istio ALPNs (or no ALPN), all mTLS modes, and various backends
	alpns := []string{"istio", "istio-peer-exchange", "istio-http/1.0", "istio-http/1.1", "istio-h2", ""}
	modes := []string{"STRICT", "PERMISSIVE", "DISABLE"}

	mtlsHost := host.Name(a.Config().FQDN())
	nakedHost := host.Name(naked.Config().FQDN())
	httpsPort := common.FindPortByName("https").ServicePort
	httpsAutoPort := common.FindPortByName("auto-https").ServicePort
	snis := []string{
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsPort),
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsPort),
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsAutoPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsAutoPort),
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsAutoPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsAutoPort),
	}
	for _, mode := range modes {
		childs := []TrafficCall{}
		for _, sni := range snis {
			for _, alpn := range alpns {
				alpn, sni, mode := alpn, sni, mode
				al := &epb.Alpn{Value: []string{alpn}}
				if alpn == "" {
					al = nil
				}
				childs = append(childs, TrafficCall{
					name: fmt.Sprintf("mode:%v,sni:%v,alpn:%v", mode, sni, alpn),
					call: eastWest.CallEcho,
					opts: echo.CallOptions{
						Port: &echo.Port{
							ServicePort: 15443,
							Protocol:    protocol.HTTPS,
						},
						ServerName: sni,
						Alpn:       al,
					},
					validator: func(resp echoclient.ParsedResponses, err error) error {
						if err == nil {
							return fmt.Errorf("expected error, but none occurred")
						}
						return nil
					},
				},
				)
			}
		}
		cases = append(cases, TrafficTestCase{
			config: globalPeerAuthentication(mode) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: cross-network-gateway-test
  namespace: istio-system
spec:
  selector:
    istio: eastwestgateway
  servers:
    - port:
        number: 15443
        name: tls
        protocol: TLS
      tls:
        mode: AUTO_PASSTHROUGH
      hosts:
        - "*.local"
`,
			children: childs,
		})
	}

	return cases
}

func globalPeerAuthentication(mode string) string {
	return fmt.Sprintf(`apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: %s
---
`, mode)
}

func TestTraffic(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			cases := []TrafficTestCase{}
			cases = append(cases, virtualServiceCases()...)
			cases = append(cases, protocolSniffingCases()...)
			cases = append(cases, trafficLoopCases()...)
			cases = append(cases, autoPassthroughCases()...)
			for _, tt := range cases {
				ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
					if len(tt.config) > 0 {
						cfg := yml.MustApplyNamespace(ctx, tt.config, echoNamespace.Name())
						ctx.Config().ApplyYAMLOrFail(ctx, "", cfg)
						defer ctx.Config().DeleteYAMLOrFail(ctx, "", cfg)
					}

					if tt.call != nil && len(tt.children) > 0 {
						ctx.Fatal("TrafficTestCase: must not specify both call and children")
					}

					if len(tt.children) > 0 {
						for _, child := range tt.children {
							ctx.NewSubTest(child.name).Run(func(ctx framework.TestContext) {
								retry.UntilSuccessOrFail(ctx, func() error {
									resp, err := child.call(child.opts)
									return child.validator(resp, err)
								}, retry.Delay(time.Millisecond*100))
							})
						}
						return
					}

					retry.UntilSuccessOrFail(ctx, func() error {
						resp, err := tt.call()
						if err != nil {
							ctx.Logf("call for %v failed, retrying: %v", tt.name, err)
							return err
						}
						return tt.validator(resp)
					}, retry.Delay(time.Millisecond*100))
				})
			}
		})
}

func almostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}
