#!env bash
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)

git apply tetrateci/patches/common/disable-dashboard.1.8.patch
git apply tetrateci/patches/common/disable-ratelimiting.1.8.patch
git apply tetrateci/patches/common/disable-vmospost.1.8.patch
git apply tetrateci/patches/common/disable-stackdriver.1.8.patch

if $(grep -q "1.17" <<< ${VERSION} ); then
  git apply tetrateci/patches/common/disable-endpointslice.1.8.patch
fi

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/patches/gke/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/patches/eks/eks-ingress.1.8.patch
fi

if [[ ${CLUSTER} == "aks" ]]; then
  git apply tetrateci/patches/aks/aks-pilot.1.8.patch
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
