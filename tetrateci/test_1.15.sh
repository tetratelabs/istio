#!/usr/bin/env bash
#
# Copyright (c) Tetrate, Inc 2022 All Rights Reserved.

set -e
set -u
set -x

SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
ROOTDIR=$( cd "${SCRIPTDIR}/.." && pwd )

"${SCRIPTDIR}/version_check.py" && exit

# shellcheck disable=SC1091
source "${SCRIPTDIR}/setup_go.sh"

COMMON_TEST_FLAGS=()

echo "Applying patches...."

# Apply the same patches that were applies when building test images
"${SCRIPTDIR}/apply_e2e_build_patches.sh"

git apply "${SCRIPTDIR}/patches/common/increase-dashboard-timeout.1.11.patch"

if [[ "${CLUSTER}" == "gke" ]]; then
  echo "Generating operator config for GKE"

  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  python3 -m pip install pyyaml --user && python3 "${SCRIPTDIR}/gen_iop.py"

  COMMON_TEST_FLAGS+=( "-istio.test.kube.helm.iopFile=${SCRIPTDIR}/iop-gke-integration.yml" )

fi

#if [[ "${CLUSTER}" == "eks" ]]; then
#  echo "Applying Ingress patch for EKS...."
#  git apply "${SCRIPTDIR}/patches/eks/eks-ingress.1.13.patch"
#fi



  go test \
    -test.v \
    -timeout 30m \
    -tags=integ \
    /tests/integration/pilot \
    --istio.test.select=-postsubmit,-flaky \
    --istio.test.ci \
    --istio.test.hub=${HUB} \
    --istio.test.tag=${TAG} \
    --istio.test.pullpolicy=IfNotPresent \
    --istio.test.retries=1 \
    --log_output_level=tf:debug \

  find /tmp -mindepth 1 -maxdepth 1 -type d -name '*istio*' -exec sudo rm -f -- {} \;
done

echo "Testing Done"
