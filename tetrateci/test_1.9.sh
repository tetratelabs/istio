#!env bash
set -e

export GOLANG_VERSION=1.15.7
./tetrateci/setup_go.sh
git apply tetrateci/patches/common/disable-dashboard.1.9.patch
git apply tetrateci/patches/common/disable-ratelimiting.1.9.patch
git apply tetrateci/patches/common/disable-stackdriver.1.9.patch

if $(grep -q "1.17" <<< ${VERSION} ); then
  git apply tetrateci/patches/common/disable-endpointslice.1.9.patch
  # somehow the code still runs even though this is not suppossed to be run for anything less than 1.18
  git apply tetrateci/patches/common/disable-ingress.1.9.patch
fi

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/patches/gke/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/patches/eks/eks-ingress.1.9.patch
fi

PACKAGES=$(go list -tags=integ ./tests/integration/... | grep -v /qualification | grep -v /examples | grep -v /multicluster)

for package in $PACKAGES; do
  n=0
  until [ "$n" -ge 3 ]
  do
    echo "========================================================TRY $n========================================================"
    go test -count=1 -p 1 -test.v -tags=integ $package -timeout 30m --istio.test.select=-postsubmit,-flaky ${CLUSTERFLAGS} && break || echo "Test Failed: $package"
    sudo rm -rf $(ls /tmp | grep istio)
    n=$((n+1))
  done
  [ "$n" -ge 3 ] && exit 1
done
