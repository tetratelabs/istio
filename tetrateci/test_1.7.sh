#!env bash
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)

git apply tetrateci/common/disable-dashboard.1.7.patch
git apply tetrateci/common/disable-multicluster.1.7.patch

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio-old/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/gke/chiron-gke.patch
  git apply disable-vmospost-gke.1.7,patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/eks/eks-ingress.1.7.patch
fi

if $(go version | grep "1.15"); then
  export GODEBUG=x509ignoreCN=0
fi

go test -count=1 ./tests/integration/... ${CLUSTERFLAGS} -p 1 -test.v -timeout 30m
