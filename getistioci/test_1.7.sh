#!env bash
set -e

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./getistioci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/getistioci/iop-gke-integration.yml"
fi

go test -tags=integ ./tests/integration/operator/...  -p 1  -test.v
go test -tags=integ -timeout 30m -run='TestEmptyCluster|TestFileOnly|TestDirectoryWithoutRecursion|TestDirectoryWithRecursion|TestInvalidFileError|TestJsonInputFile|TestJsonOutput|TestKubeOnly|TestFileAndKubeCombined|TestAllNamespaces|TestTimeout|TestErrorLine|TestWait|TestVersion|TestDescribe|TestAddToAndRemoveFromMesh|TestProxyConfig|TestProxyStatus|TestAuthZCheck|TestLocality|TestMain|TestMirroring|TestMirroringExternalService|TestTproxy|TestValidation|TestEnsureNoMissingCRDs|TestWebhook' ./tests/integration/pilot/ -p 1 -test.v 
go test -tags=integ ./tests/integration/pilot/analysis/... -p 1 -test.v
go test -tags=integ ./tests/integration/pilot/revisions/... -p 1 -test.v
go test -tags=integ -timeout 30m -run='TestStatsFilter|TestStatsTCPFilter|TestSetup|TestIstioctlMetrics|TestTcpMetric|TestStatsFilter|TestWASMTcpMetric|TestWasmStatsFilter|TestMain|TestCustomizeMetrics' ./tests/integration/telemetry/stats/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/ca_custom_root/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/filebased_tls_origination/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/mtls_first_party_jwt/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/mtlsk8sca/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/sds_egress/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/sds_tls_origination/... -p 1 -test.v
go test -tags=integ ./tests/integration/security/webhook/... -p 1 -test.v

if [[ ${CLUSTER} == "eks" ]]; then
  go test -tags=integ ./tests/integration/security/chiron/... -p 1 -test.v
fi

if [[ $CLUSTER != "aks" ]]; then
  go test -tags=integ ./tests/integration/pilot/cni/... ${CLUSTERFLAGS} -p 1 -test.v
fi
