#!env bash
set -e

git apply tetrateci/common/disable-dashboard.1.9.patch
git apply tetrateci/common/disable-multicluster.1.9.patch
git apply tetrateci/common/disable-ratelimiting.1.9.patch
git apply tetrateci/common/disable-vmospost.1.9.patch
git apply tetrateci/common/disable-stackdriver.1.9.patch

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/chiron-gke.patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/eks/eks-ingress.1.9.patch
fi

go test -count=1 ./tests/integration/... ${CLUSTERFLAGS} -p 1 -test.v -tags="integ" -timeout 30m