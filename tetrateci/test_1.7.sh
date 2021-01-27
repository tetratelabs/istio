#!env bash
set -e

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/chiron-gke.patch
fi

go test -count=1 -tags=integ ./tests/integration/operator/...  -p 1  -test.v
go test -count=1 -tags=integ ./tests/integration/galley/...  -p 1  -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/analysis/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/locality/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/revisions/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/mixer/outboundtrafficpolicy  -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/telemetry/outboundtrafficpolicy -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/ca_custom_root/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/cert_provision_prometheus/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/chiron/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/filebased_tls_origination/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/mtls_first_party_jwt/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/mtlsk8sca/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/sds_egress/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/sds_tls_origination/... -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/webhook/... -p 1 -test.v

if [[ ${CLUSTER} == "eks" ]]; then
  go test -count=1 -tags=integ -timeout 30m -run='TestWait|TestVersion|TestDescribe|TestAddToAndRemoveFromMesh|TestProxyConfig|TestProxyStatus|TestAuthZCheck|TestMain|TestMirroring|TestMirroringExternalSerivce|TestTraffic' ./tests/integration/pilot/ -p 1 -test.v 
  go test -count=1 -tags=integ -run 'TestAuthorization_mTLS|TestAuthorization_JWT|TestAuthorization_WorkloadSelector|TestAuthorization_Deny|TestAuthorization_NegativeMatch|TestAuthorization_EgressGateway|TestAuthorization_TCP|TestAuthorization_Conditions|TestAuthorization_GRPC|TestAuthorization_Path|TestRequestAuthentication|TestMain|TestMtlsHealthCheck|TestPassThroughFilterChain' ./tests/integration/security/.  -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m -run='TestStatsFilter|TestSetup|TestIstioctlMetrics|TestStatsFilter|TestWASMTcpMetric|TestWasmStatsFilter|TestMain|TestCustomizeMetrics' ./tests/integration/telemetry/stats/... -p 1 -test.v
else
  go test -count=1 -tags=integ ./tests/integration/mixer/. -p 1 -test.v
  go test -count=1 -tags=integ ./tests/integration/mixer/envoy/...  -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/mixer/policy/. -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/mixer/telemetry/... -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/pilot/. -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/pilot/vm/. -run='TestTrafficShifting|TestVmOS' -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/pilot/ingress/. -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/security/.  -p 1 -test.v
  go test -count=1 -tags=integ ./tests/integration/security/sds_ingress/.  -p 1 -test.v
  go test -count=1 -tags=integ ./tests/integration/security/sds_ingress_k8sca/.  -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/telemetry/. -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/telemetry/stats/... -p 1 -test.v
fi

if [[ $CLUSTER != "aks" ]]; then
  go test -count=1 -tags=integ ./tests/integration/pilot/cni/... ${CLUSTERFLAGS} -p 1 -test.v
fi
