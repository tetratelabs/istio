#!/usr/bin/env bash

# this is just for reference won't be used
# master for now
git clone https://github.com/istio/istio
cd istio/test/integration/

go test -tags=integ ./operator/...  -istio.test.skipVM true -p 1
go test -tags=integ ./helm/... -istio.test.skipVM true -p 1
go test -tags=integ -run='TestEmptyCluster|TestFileOnly|TestDirectoryWithoutRecursion|TestDirectoryWithRecursion|TestInvalidFileError|TestJsonInputFile|TestJsonOutput|TestKubeOnly|TestFileAndKubeCombined|TestAllNamespaces|TestTimeout|TestErrorLine|TestWait|TestVersion|TestDescribe|TestAddToAndRemoveFromMesh|TestProxyConfig|TestProxyStatus|TestAuthZCheck|TestLocality|TestMain|TestMirroring|TestMirroringExternalService|TestTproxy|TestRevisionedUpgrade|TestValidation|TestEnsureNoMissingCRDs|TestWebhook' ./pilot/ -istio.test.skipVM true -p 1
go test -tags=integ ./pilot/analysis/... -istio.test.skipVM true -p 1
go test -tags=integ ./pilot/cni/... -istio.test.skipVM true -p 1 
go test -tags=integ ./pilot/revisions/... -istio.test.skipVM true -p 1
go test -tags=integ -run='TestStatsFilter|TestStatsTCPFilter|TestSetup|TestIstioctlMetrics|TestTcpMetric|TestStatsFilter|TestWASMTcpMetric|TestWasmStatsFilter|TestMain|TestCustomizeMetrics'  ./telemetry/stats/... -istio.test.skipVM true -p 1
go test -tags=integ  ./security/ca_custom_root/... -istio.test.skipVM true -p 1
go test -tags=integ  ./security/chiron/... -istio.test.skipVM true -p 1 
go test -tags=integ  ./security/file_mounted_certs/... -istio.test.skipVM true -p 1 
go test -tags=integ  ./security/filebased_tls_origination/... -istio.test.skipVM true -p 1 
go test -tags=integ  ./security/mtls_first_party_jwt/... -istio.test.skipVM true -p 1
go test -tags=integ  ./security/mtlsk8sca/... -istio.test.skipVM true -p 1
go test -tags=integ  ./security/sds_egress/... -istio.test.skipVM true -p 1
go test -tags=integ  ./security/sds_tls_origination/... -istio.test.skipVM true -p 1 
go test -tags=integ  ./security/webhook/... -istio.test.skipVM true -p 1 
