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

package metrics

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	util "istio.io/istio/tests/integration/mixer"
)

var (
	ist        istio.Instance
	bookinfoNs namespace.Instance
	ing        ingress.Instance
	prom       prometheus.Instance
)

// This file contains Mixer tests that are ported from Mixer E2E tests

// Port of TestMetric
func TestIngessToPrometheus_ServiceMetric(t *testing.T) {
	framework.
		NewTest(t).
		// TODO(https://github.com/istio/istio/issues/14819)
		Label(label.Flaky).
		Run(func(ctx framework.TestContext) {
			label := "source_workload"
			labelValue := "istio-ingressgateway"
			testMetric(t, ctx, label, labelValue)
		})
}

// Port of TestMetric
func TestIngessToPrometheus_IngressMetric(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("SetupAndPrometheus").
				Run(func(ctx framework.TestContext) {
					label := "destination_service"
					labelValue := "productpage.{{.TestNamespace}}.svc.cluster.local"
					testMetric(t, ctx, label, labelValue)
				})
		})
}

func testMetric(t *testing.T, ctx framework.TestContext, label string, labelValue string) { // nolint:interfacer
	t.Helper()
	ctx.Config().ApplyYAMLOrFail(
		t,
		bookinfoNs.Name(),
		bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
	)
	defer ctx.Config().DeleteYAMLOrFail(t,
		bookinfoNs.Name(),
		bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
		bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, bookinfoNs.Name()))

	util.AllowRuleSync(t)

	// Warm up
	addr := ing.HTTPAddress()
	url := fmt.Sprintf("http://%s/productpage", addr.String())
	res := util.SendTraffic(ing, t, "Sending traffic", url, "", 10)
	if res.RetCodes[200] < 1 {
		t.Fatalf("unable to retrieve 200 from product page: %v", res.RetCodes)
	}

	label = tmpl.EvaluateOrFail(t, label, map[string]string{"TestNamespace": bookinfoNs.Name()})
	labelValue = tmpl.EvaluateOrFail(t, labelValue, map[string]string{"TestNamespace": bookinfoNs.Name()})

	// Wait for some data to arrive.
	initial, err := prom.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Baseline established: initial = %v", initial)

	res = util.SendTraffic(ing, t, "Sending traffic", url, "", 10)
	if res.RetCodes[200] < 1 {
		t.Fatalf("unable to retrieve 200 from product page: %v", res.RetCodes)
	}

	final, err := prom.WaitForQuiesce(`istio_requests_total{%s=%q,response_code="200"}`, label, labelValue)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Quiesced to: final = %v", final)

	metricName := "istio_requests_total"
	i, err := prom.Sum(initial, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, util.PromDump(prom, metricName))
		t.Fatal(err)
	}

	f, err := prom.Sum(final, nil)
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, util.PromDump(prom, metricName))
		t.Fatal(err)
	}

	// We should see 10 requests but giving an error of 1, to make test less flaky.
	if (f - i) < float64(9) {
		t.Errorf("Bad metric value: got %f, want at least 9", f-i)
	}
}

// Port of TestTcpMetric
func TestTcpMetric(t *testing.T) {
	framework.
		NewTest(t).
		// TODO(https://github.com/istio/istio/issues/18105)
		Label(label.Flaky).
		Run(func(ctx framework.TestContext) {
			undeploy1 := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookinfoRatingsv2})
			undeploy2 := bookinfo.DeployOrFail(t, ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookinfoDB})
			defer undeploy1()
			defer undeploy2()

			ctx.Config().ApplyYAMLOrFail(
				t,
				bookinfoNs.Name(),
				bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingTCPDbRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			)
			defer ctx.Config().DeleteYAMLOrFail(
				t,
				bookinfoNs.Name(),
				bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingTCPDbRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			)

			util.AllowRuleSync(t)

			addr := ing.HTTPAddress()
			url := fmt.Sprintf("http://%s/productpage", addr.String())
			res := util.SendTraffic(ing, t, "Sending traffic", url, "", 10)
			if res.RetCodes[200] < 1 {
				t.Fatalf("unable to retrieve 200 from product page: %v", res.RetCodes)
			}

			query := fmt.Sprintf("sum(istio_tcp_sent_bytes_total{destination_app=\"%s\"})", "mongodb")
			util.ValidateMetric(t, prom, query, "istio_tcp_sent_bytes_total", 1)

			query = fmt.Sprintf("sum(istio_tcp_received_bytes_total{destination_app=\"%s\"})", "mongodb")
			util.ValidateMetric(t, prom, query, "istio_tcp_received_bytes_total", 1)

			query = fmt.Sprintf("sum(istio_tcp_connections_opened_total{destination_app=\"%s\"})", "mongodb")
			util.ValidateMetric(t, prom, query, "istio_tcp_connections_opened_total", 1)

			query = fmt.Sprintf("sum(istio_tcp_connections_closed_total{destination_app=\"%s\"})", "mongodb")
			util.ValidateMetric(t, prom, query, "istio_tcp_connections_closed_total", 1)
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, func(cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  prometheus:	
    enabled: true
  meshConfig:
    disablePolicyChecks: false
  telemetry:
    v1:
      enabled: true
    v2:
      enabled: false
components:
  policy:
    enabled: true
  telemetry:
    enabled: true`
		})).
		Setup(testsetup).
		Run()
}

func testsetup(ctx resource.Context) (err error) {
	bookinfoNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return
	}
	if _, err := bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo}); err != nil {
		return err
	}
	ing, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return err
	}
	prom, err = prometheus.New(ctx, prometheus.Config{
		SkipDeploy: true, // Use istioctl prometheus; sample prometheus does not support mixer.
	})
	if err != nil {
		return err
	}
	yamlText, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNs.Name())
	if err != nil {
		return err
	}
	err = ctx.Config().ApplyYAML(bookinfoNs.Name(), yamlText)
	if err != nil {
		return err
	}

	return nil
}
