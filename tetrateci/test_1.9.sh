#!/usr/bin/env bash
set -e

./tetrateci/setup_go.sh

echo "Applying patches...."
git apply tetrateci/patches/common/disable-dashboard.1.9.patch
git apply tetrateci/patches/common/disable-ratelimiting.1.9.patch
git apply tetrateci/patches/common/disable-stackdriver.1.9.patch
git apply tetrateci/patches/common/increase-vm-timeout.1.9.patch

if $(grep -q "1.17" <<< ${VERSION} || grep -q "1.16" <<< ${VERSION}); then
  git apply tetrateci/patches/common/disable-endpointslice.1.9.patch
  # somehow the code still runs even though this is not suppossed to be run for anything less than 1.18
  git apply tetrateci/patches/common/disable-ingress.1.9.patch
fi

if [[ ${CLUSTER} == "gke" ]]; then
  echo "Generating operator config for GKE"
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  echo "Applying GKE specific patches...."
  git apply tetrateci/patches/gke/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  echo "Applying Ingress patch for EKS...."
  git apply tetrateci/patches/eks/eks-ingress.1.9.patch
fi

PACKAGES=$(go list -tags=integ ./tests/integration/... | grep -v /qualification | grep -v /examples | grep -v /multicluster)

echo "Starting Testing"

for package in $PACKAGES; do
  n=0
  until [ "$n" -ge 3 ]
  do
    echo "========================================================TESTING $package | TRY $n========================================================"
    go test -p 1 -test.v -tags=integ $package -timeout 30m --istio.test.select=-postsubmit,-flaky ${CLUSTERFLAGS} && break || echo "Test Failed: $package"
    for folder in $(ls -d /tmp/* | grep istio); do sudo rm -rf -- $folder; done
    n=$((n+1))
  done
  [ "$n" -ge 3 ] && exit 1
done

echo "Testing Done"
