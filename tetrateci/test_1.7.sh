#!env bash
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/eks-ingress.1.7.patch
fi

go test -count=1 ./tests/integration/operator/...  -p 1  -test.v

go test -count=1 ./tests/integration/galley/...  -p 1  -test.v

go test -count=1 ./tests/integration/pilot/analysis/... -p 1 -test.v
go test -count=1 ./tests/integration/pilot/locality/... -p 1 -test.v
go test -count=1 ./tests/integration/pilot/revisions/... -p 1 -test.v
go test -count=1 -timeout 30m ./tests/integration/pilot/. -p 1 -test.v
# TestVmOSPost fails in gke
go test -count=1 -timeout 30m ./tests/integration/pilot/vm/. -p 1 -test.v -run "TestVmOS"
# Note: if this compains about unknown field namespaceSelector, clean the x-k8s crds
go test -count=1 -timeout 30m ./tests/integration/pilot/ingress/. -p 1 -test.v
go test -count=1 ./tests/integration/pilot/cni/... ${CLUSTERFLAGS} -p 1 -test.v

go test -count=1 ./tests/integration/mixer/outboundtrafficpolicy  -p 1 -test.v
go test -count=1 -timeout 30m ./tests/integration/mixer/. -p 1 -test.v
go test -count=1 ./tests/integration/mixer/envoy/...  -p 1 -test.v
go test -count=1 -timeout 30m ./tests/integration/mixer/policy/. -p 1 -test.v
go test -count=1 -timeout 30m ./tests/integration/mixer/telemetry/... -p 1 -test.v

go test -count=1 ./tests/integration/telemetry/outboundtrafficpolicy -p 1 -test.v
go test -count=1 -timeout 30m ./tests/integration/telemetry/. -p 1 -test.v
# TestIstioCtlMetrics fails everywhere
go test -count=1 -timeout 30m ./tests/integration/telemetry/stats/... -p 1 -test.v -run "TestStatsFilter|TestSetup|TestWasmStatsFilter|TestTcpMetric"
go test -count=1 -tags=integ ./tests/integration/telemetry/tracing/...  -p 1 -test.v

go test -count=1 ./tests/integration/security/ca_custom_root/... -p 1 -test.v
go test -count=1 ./tests/integration/security/cert_provision_prometheus/... -p 1 -test.v
go test -count=1 ./tests/integration/security/chiron/... -p 1 -test.v
go test -count=1 ./tests/integration/security/filebased_tls_origination/... -p 1 -test.v
go test -count=1 ./tests/integration/security/mtls_first_party_jwt/... -p 1 -test.v
go test -count=1 ./tests/integration/security/mtlsk8sca/... -p 1 -test.v
go test -count=1 ./tests/integration/security/sds_egress/... -p 1 -test.v
go test -count=1 ./tests/integration/security/sds_tls_origination/... -p 1 -test.v
go test -count=1 ./tests/integration/security/webhook/... -p 1 -test.v
go test -count=1 -timeout 30m ./tests/integration/security/.  -p 1 -test.v
go test -count=1 ./tests/integration/security/sds_ingress/.  -p 1 -test.v
go test -count=1 ./tests/integration/security/sds_ingress_k8sca/.  -p 1 -test.v
