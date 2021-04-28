#!/usr/bin/env bash
./tetrateci/version_check.py && exit
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)
echo "Set REPO_ROOT=$REPO_ROOT"
source ./tetrateci/setup_go.sh

echo "Applying patches...."

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio-old/issues/76
  echo "Generating operator config for GKE"
  python3 -m pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"

  echo "Applying GKE specific patches...."
  git apply tetrateci/patches/gke/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  echo "Applying Ingress patch for EKS...."
  git apply tetrateci/patches/eks/eks-ingress.1.7.patch
fi

PACKAGES=$(go list ./tests/integration/... | grep -v /qualification | grep -v /examples | grep -v /multicluster | grep -v /stackdriver)

echo "Starting Testing"

for package in $PACKAGES; do
  n=0
  until [ "$n" -ge 3 ]
  do
    echo "========================================================TESTING $package | TRY $n========================================================"
    go test -test.v $package -timeout 30m --istio.test.select=-postsubmit,-flaky --istio.test.ci --istio.test.pullpolicy IfNotPresent ${CLUSTERFLAGS} && break || echo "Test Failed: $package"
    for folder in $(ls -d /tmp/* | grep istio); do sudo rm -rf -- $folder; done
    n=$((n+1))
  done
  [ "$n" -ge 3 ] && exit 1
done

echo "Testing Done"
