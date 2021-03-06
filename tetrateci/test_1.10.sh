#!/usr/bin/env bash
./tetrateci/version_check.py && exit
set -e

source ./tetrateci/setup_go.sh

echo "Applying patches...."

# git apply tetrateci/patches/common/increase-vm-timeout.1.9.patch
# git apply tetrateci/patches/common/increase-sniffing-timeout.1.9.patch
git apply tetrateci/patches/common/increase-dashboard-timeout.1.10.patch
git apply tetrateci/patches/common/disable-vmregistration.1.10.patch # https://github.com/istio/istio/issues/29100
git apply tetrateci/patches/common/disable-passthroughfilterchain.1.10.patch # https://github.com/istio/istio/issues/32623

# the code fails whenever there is something other than digits in the k8s minor version
# in our case which is a "+" symbol due to extra patching by corresponding vendor
# so we get 1.17+ instead of 1.17
# git apply tetrateci/patches/common/fix-version-check.1.9.patch

if [[ ${CLUSTER} == "gke" ]]; then
  echo "Generating operator config for GKE"
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  python3 -m pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"

  echo "Applying GKE specific patches...."
  git apply tetrateci/patches/gke/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  echo "Applying Ingress patch for EKS...."
  git apply tetrateci/patches/eks/eks-ingress.1.10.patch
fi

if $(grep -q "1.17" <<< ${K8S_VERSION}); then
  PACKAGES=$(go list -tags=integ ./tests/integration/... | grep -v /qualification | grep -v /examples | grep -v /multicluster | grep -v /endpointslice | grep -v /stackdriver)
else
  PACKAGES=$(go list -tags=integ ./tests/integration/... | grep -v /qualification | grep -v /examples | grep -v /multicluster | grep -v /stackdriver)
fi

echo "Starting Testing"

for package in $PACKAGES; do
  n=0
  until [ "$n" -ge 3 ]
  do
    echo "========================================================TESTING $package | TRY $n========================================================"
    go test -test.v -tags=integ $package -timeout 30m --istio.test.select=-postsubmit,-flaky --istio.test.ci --istio.test.pullpolicy IfNotPresent ${CLUSTERFLAGS} && break || echo "Test Failed: $package"
    for folder in $(ls -d /tmp/* | grep istio); do sudo rm -rf -- $folder; done
    n=$((n+1))
  done
  [ "$n" -ge 3 ] && exit 1
done

echo "Testing Done"
