#!env bash
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)

git apply tetrateci/common/*.patch

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio-old/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/gke/*.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/eks/*.patch
fi

go test -count=1 ./tests/integration/... ${CLUSTERFLAGS} -p 1 -test.v
