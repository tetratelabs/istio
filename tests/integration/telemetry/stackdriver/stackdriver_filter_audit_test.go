// +build integ
// Copyright Istio Authors. All Rights Reserved.
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

package stackdriver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	serverAuditAllLogEntry = "testdata/security_authz_audit/server_audit_all_log.json.tmpl"
	serverAuditFooLogEntry = "testdata/security_authz_audit/server_audit_foo_log.json.tmpl"
	serverAuditBarLogEntry = "testdata/security_authz_audit/server_audit_bar_log.json.tmpl"
	auditPolicyForLogEntry = "testdata/security_authz_audit/v1beta1-audit-authorization-policy.yaml.tmpl"
)

// TestStackdriverAuditLogging testing Authz Policy can config stackdriver with audit policy
func TestStackdriverHTTPAuditLogging(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())

			ns := getEchoNamespaceInstance().Name()
			args := map[string]string{
				"Namespace": ns,
			}
			policies := tmpl.EvaluateAllOrFail(t, args, file.AsStringOrFail(t, auditPolicyForLogEntry))
			ctx.Config().ApplyYAMLOrFail(t, ns, policies...)
			defer ctx.Config().DeleteYAMLOrFail(t, ns, policies...)
			t.Logf("Audit policy deployed to namespace %v", ns)

			for _, cltInstance := range clt {
				scopes.Framework.Infof("Validating Audit policy and Telemetry for Cluster %v", cltInstance.Config().Cluster.Name())
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := sendTrafficForAudit(t, cltInstance); err != nil {
							return err
						}
						t.Logf("Traffic sent to namespace %v", ns)

						clName := cltInstance.Config().Cluster.Name()
						t.Logf("Collect Audit Log for cluster %v", clName)

						var errs []string

						errAuditFoo := validateLogs(t, serverAuditFooLogEntry, clName, stackdriver.ServerAuditLog)
						if errAuditFoo == nil {
							t.Logf("Foo Audit Log validated for cluster %v", clName)
						} else {
							errs = append(errs, errAuditFoo.Error())
						}

						errAuditBar := validateLogs(t, serverAuditBarLogEntry, clName, stackdriver.ServerAuditLog)
						if errAuditBar == nil {
							t.Logf("Bar Audit Log validated for cluster %v", clName)
						} else {
							errs = append(errs, errAuditBar.Error())
						}

						errAuditAll := validateLogs(t, serverAuditAllLogEntry, clName, stackdriver.ServerAuditLog)
						if errAuditAll == nil {
							t.Logf("All Audit Log validated for cluster %v", clName)
						} else {
							errs = append(errs, errAuditAll.Error())
						}

						entries, err := sdInst.ListLogEntries(stackdriver.ServerAuditLog)
						if err != nil {
							errs = append(errs, err.Error())
						} else {
							for _, l := range entries {
								if l.HttpRequest != nil && strings.HasSuffix(l.HttpRequest.RequestUrl, "audit-none") {
									errs = append(errs, "unwanted audit log entry `/audit-none` received.")
								}
							}
						}

						if len(errs) == 0 {
							return nil
						}

						return fmt.Errorf(strings.Join(errs, "\n"))
					}, retry.Delay(5*time.Second), retry.Timeout(80*time.Second))

					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

// send http requests with different header and path
func sendTrafficForAudit(t *testing.T, cltInstance echo.Instance) error {
	t.Helper()

	newOptions := func(headers http.Header, path string) echo.CallOptions {
		return echo.CallOptions{
			Target:   srv[0],
			PortName: "http",
			Headers:  headers,
			Path:     path,
			Count:    telemetry.RequestCountMultipler,
		}
	}

	opts := []echo.CallOptions{
		// request will be logged if "request header" value and "to operation path" is matched with audit policy
		// path "/audit-none" will be filtered by audit policy and will not be logged
		newOptions(nil, "/audit-none"),
		newOptions(map[string][]string{"X-Audit": {"foo"}}, "/audit-none"),
		newOptions(map[string][]string{"x-Audit": {"bar"}}, "/audit-none"),

		// Headers are case sensitive for this test framework. It requires capitalize the first letter of every word
		newOptions(map[string][]string{"X-Header": {"bar"}}, "/foo"),
		newOptions(map[string][]string{"X-Header": {"foo"}}, "/bar"),
		newOptions(map[string][]string{"X-Header": {"bar"}}, "/bar"),
		newOptions(map[string][]string{"X-Header": {"foo"}}, "/foo"),

		// path "/audit-all" is matched in audit policy and all requests will be logged
		newOptions(nil, "/audit-all"),
		newOptions(map[string][]string{"X-Audit": {"foo"}}, "/audit-all"),
		newOptions(map[string][]string{"X-Audit": {"bar"}}, "/audit-all"),
	}

	for _, opt := range opts {
		if _, err := cltInstance.Call(opt); err != nil {
			t.Logf("with call option %v got err %v", opt, err)
			return err
		}
	}
	return nil
}
