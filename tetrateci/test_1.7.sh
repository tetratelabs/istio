#!env bash
set -e

# need this variable to run the tests outside GOPATH
export REPO_ROOT=$(pwd)

git apply tetrateci/patches/common/disable-dashboard.1.7.patch
git apply tetrateci/patches/common/disable-multicluster.1.7.patch
git apply tetrateci/patches/common/disable-stackdriver.1.7.patch

if [[ ${CLUSTER} == "gke" ]]; then
  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio-old/issues/76
  pip install pyyaml --user && ./tetrateci/gen_iop.py
  CLUSTERFLAGS="-istio.test.kube.helm.iopFile $(pwd)/tetrateci/iop-gke-integration.yml"
  git apply tetrateci/patches/gke/chiron-gke.patch
  git apply tetrateci/patches/gke/disable-vmospost-gke.1.7,patch
fi

if [[ ${CLUSTER} == "eks" ]]; then
  git apply tetrateci/patches/eks/eks-ingress.1.7.patch
fi

if $(go version | grep "1.15"); then
  export GODEBUG=x509ignoreCN=0
fi

PACKAGES=$(go list -tags=integ ./tests/integration/... | grep -v /qualification | grep -v /examples | grep -v /multicluster)

for package in $PACKAGES; do
  n=0
  until [ "$n" -ge 3 ]
  do
    n=$((n+1))
    sleep 15
    echo "========================================================TRY $n========================================================"
    go test -count=1 -p 1 -test.v -tags=integ $package -timeout 30m --istio.test.select=-postsubmit,-flaky ${CLUSTERFLAGS} && break || echo "Test Failed: $package"
    sudo rm -rf $(ls /tmp | grep istio)
  done

  [ "$n" -ge 3 ] && exit 1
  
done
