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

package nullvm

import (
	"testing"

	"istio.io/istio/pkg/test/framework/features"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

func TestTcpMetric(t *testing.T) { // nolint:interfacer
	common.TestStatsTCPFilter(t, features.Feature("observability.telemetry.stats.prometheus.tcp"))
}
