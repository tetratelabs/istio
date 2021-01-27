#!env bash
set -e

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/chiron-gke.patch
fi

go test -count=1 -tags=integ ./tests/integration/helm/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/operator/...  -istio.test.skipVM true -p 1  -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/analysis/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/revisions/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/telemetry/requestclassification/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/telemetry/outboundtrafficpolicy/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/ca_custom_root/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/ecc_signature_algorithm/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/chiron/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/filebased_tls_origination/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/mtls_first_party_jwt/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/mtlsk8sca/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/sds_egress/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/sds_tls_origination/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/webhook/... -istio.test.skipVM true -p 1 -test.v

if [[ ${CLUSTER} == "eks" ]]; then
 go test -count=1 -tags=integ -timeout 30m ./tests/integration/telemetry/stats/... -run='TestStatsFilter|TestSetup|TestIstioctlMetrics|TestStatsFilter|TestWASMTcpMetric|TestWasmStatsFilter|TestMain|TestCustomizeMetrics' -istio.test.skipVM true -p 1 -test.v
 go test -count=1 -tags=integ -timeout 30m ./tests/integration/pilot/ -run='TestEmptyCluster|TestFileOnly|TestDirectoryWithoutRecursion|TestDirectoryWithRecursion|TestInvalidFileError|TestJsonInputFile|TestJsonOutput|TestKubeOnly|TestFileAndKubeCombined|TestAllNamespaces|TestTimeout|TestErrorLine|TestWait|TestVersion|TestDescribe|TestAddToAndRemoveFromMesh|TestProxyConfig|TestProxyStatus|TestAuthZCheck|TestLocality|TestMain|TestMirroring|TestMirroringExternalService|TestTproxy|TestValidation|TestEnsureNoMissingCRDs|TestWebhook' -istio.test.skipVM true -p 1 -test.v
else
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/security/. -istio.test.skipVM true -p 1 -test.v
  go test -count=1 -tags=integ ./tests/integration/security/sds_ingress/. -istio.test.skipVM true -p 1 -test.v
  go test -count=1 -tags=integ ./tests/integration/security/sds_ingress_gateway/. -istio.test.skipVM true -p 1 -test.v
  go test -count=1 -tags=integ ./tests/integration/security/sds_ingress_k8sca/. -istio.test.skipVM true -p 1 -test.v
  go test -count=1 -tags=integ -timeout 30m ./tests/integration/pilot/ -run='TestAddToAndRemoveFromMesh|TestAllNamespaces|TestAuthZCheck|TestDescribe|TestDirectoryWithRecursion|TestDirectoryWithoutRecursion|TestEmptyCluster|TestEnsureNoMissingCRDs|TestErrorLine|TestFileAndKubeCombined|TestFileOnly|TestGateway|TestIngress|TestInvalidFileError|TestIstioctlMetrics|TestJsonInputFile|TestJsonOutput|TestKubeOnly|TestLocality|TestMirroring|TestMirroringExternalService|TestProxyConfig|TestProxyStatus|TestStatsFilter|TestTcpMetric|TestTraffic|TestValidation|TestVersion|TestWASMTcpMetric|TestWait|TestWasmStatsFilter|TestWebhook' -istio.test.skipVM true -p 1 -test.v

  if [[ ${CLUSTER} == "aks" ]]; then
    go test -count=1 -tags=integ -timeout 30m -run='TestStatsFilter|TestStatsTCPFilter|TestSetup|TestIstioctlMetrics|TestTcpMetric|TestStatsFilter|TestWASMTcpMetric|TestWasmStatsFilter|TestMain|TestCustomizeMetrics' ./tests/integration/telemetry/stats/... -istio.test.skipVM true -p 1 -test.v
  else
    go test -count=1 -tags=integ ./tests/integration/telemetry/stackdriver/... -run='TestStackdriverHTTPAuditLogging|TestVMTelemetry' -istio.test.skipVM true -p 1 -test.v
    go test -count=1 -tags=integ -timeout 30m ./tests/integration/telemetry/stats/... -istio.test.skipVM true -p 1 -test.v
  fi
fi

if [[ ${CLUSTER} != "aks" ]]; then
  go test -count=1 -tags=integ ./tests/integration/pilot/cni/... ${CLUSTERFLAGS} -istio.test.skipVM true -p 1 -test.v
fi
