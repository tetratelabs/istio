#!env bash
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)

git apply tetrateci/common/disable-dashboard.1.8.patch
git apply tetrateci/common/disable-multicluster.1.8.patch
git apply tetrateci/common/disable-ratelimiting.1.8.patch
git apply tetrateci/common/disable-vmospost.1.8.patch
# the code only gets triggered for 1.17 k8s so no explicit version checking required from our side
git apply tetrateci/common/disable-endpointslice.1.8.patch

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/eks/eks-ingress.1.8.patch
fi

if [[ ${CLUSTER} == "aks" ]]; then
  # Just increasing the timeout though the test is disabled for now
  git apply tetrateci/aks/aks-pilot.1.8.patch
fi

if $(go version | grep "1.15"); then
  export GODEBUG=x509ignoreCN=0
fi

go test -count=1 ./tests/integration/... ${CLUSTERFLAGS} -p 1 -test.v