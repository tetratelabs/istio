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
go test -count=1 -tags=integ -timeout 30m -run='TestEmptyCluster|TestFileOnly|TestDirectoryWithoutRecursion|TestDirectoryWithRecursion|TestInvalidFileError|TestJsonInputFile|TestJsonOutput|TestKubeOnly|TestFileAndKubeCombined|TestAllNamespaces|TestTimeout|TestErrorLine|TestWait|TestVersion|TestDescribe|TestAddToAndRemoveFromMesh|TestProxyConfig|TestProxyStatus|TestAuthZCheck|TestLocality|TestMain|TestMirroring|TestMirroringExternalService|TestTproxy|TestValidation|TestEnsureNoMissingCRDs|TestWebhook' ./tests/integration/pilot/ -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/analysis/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/pilot/revisions/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ -timeout 30m -run='TestStatsFilter|TestStatsTCPFilter|TestSetup|TestIstioctlMetrics|TestTcpMetric|TestStatsFilter|TestWASMTcpMetric|TestWasmStatsFilter|TestMain|TestCustomizeMetrics' ./tests/integration/telemetry/stats/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/tracing/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/requestclassification/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/policy/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/outboundtrafficpolicy/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/ca_custom_root/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/chiron/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/file_mounted_certs/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/filebased_tls_origination/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/mtls_first_party_jwt/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/mtlsk8sca/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/sds_egress/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/sds_tls_origination/... -istio.test.skipVM true -p 1 -test.v
go test -count=1 -tags=integ ./tests/integration/security/webhook/... -istio.test.skipVM true -p 1 -test.v

if [[ $CLUSTER != "aks" ]]; then
  go test -count=1 -tags=integ ./tests/integration/pilot/cni/... ${CLUSTERFLAGS} -istio.test.skipVM true -p 1 -test.v
fi
