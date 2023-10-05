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

#git apply "${SCRIPTDIR}/patches/common/increase-dashboard-timeout.1.11.patch"

if [[ "${CLUSTER}" == "gke" ]]; then
  echo "Generating operator config for GKE"

  # Overlay CNI Parameters for GCP : https://github.com/tetratelabs/getistio/issues/76
  python3 -m pip install pyyaml --user && python3 "${SCRIPTDIR}/gen_iop.py"

  COMMON_TEST_FLAGS+=( "-istio.test.kube.helm.iopFile=${SCRIPTDIR}/iop-gke-integration.yml" )

fi

#if [[ "${CLUSTER}" == "eks" ]]; then
#  echo "Applying  patch for EKS...."
#  git apply "${SCRIPTDIR}/patches/eks/eks_${ISTIO_MINOR_VER}.patch"
#fi

#go test -test.v -timeout 2h -tags=integ istio.io/istio/tests/integration/security --istio.test.select=-postsubmit,-flaky  --istio.test.ci --istio.test.hub=${HUB} --istio.test.tag=${TAG}-distroless --istio.test.pullpolicy=IfNotPresent --istio.test.retries=1 && go test -test.v -timeout 2h -tags=integ istio.io/istio/tests/integration/security --istio.test.select=-postsubmit,-flaky  --istio.test.ci --istio.test.hub=${HUB} --istio.test.tag=${TAG} --istio.test.pullpolicy=IfNotPresent

PACKAGES=$(go list -tags=integ "${ROOTDIR}/tests/integration/...")

echo "Starting Testing"

FAILED_PACKAGES=()

for pkg in $PACKAGES; do
  echo "========================================================TESTING ${pkg} ========================================================"

  SKIP_RULE=$( grep -F "${pkg}=" "${SCRIPTDIR}/${ISTIO_MINOR_VER}/test/skip.d/${CLUSTER}" 2>/dev/null || echo "" )
  SKIP_TESTS=$( echo -n "${SKIP_RULE#${pkg}=}" )

  if [[ "${SKIP_TESTS}" == "*" ]]; then
    echo "Skipping according to the rule: ${SKIP_RULE}"
    continue
  fi

  read -ra SKIP_TESTS_ARRAY <<< "${SKIP_TESTS}"

  SKIP_TEST_FLAGS=()
  for test in ${SKIP_TESTS_ARRAY[@]+"${SKIP_TESTS_ARRAY[@]}"} ; do
    SKIP_TEST_FLAGS+=( "--istio.test.skip=${test}" )
  done

  go test \
    -test.v \
    -timeout 2h \
    -tags=integ \
    "${pkg}" \
    --istio.test.select=-postsubmit,-flaky \
    ${SKIP_TEST_FLAGS[@]+"${SKIP_TEST_FLAGS[@]}"} \
    --istio.test.ci \
    --istio.test.skipVM=true \
    --istio.test.hub=${HUB} \
    --istio.test.tag=${TAG}-distroless \
    --istio.test.pullpolicy=IfNotPresent \
    --istio.test.retries=1 \
    ${COMMON_TEST_FLAGS[@]+"${COMMON_TEST_FLAGS[@]}"} \
    || \
    { FAILED_PACKAGES+=( "${pkg}" ) && echo "Test Failed: ${pkg}" ; }

  find /tmp -mindepth 1 -maxdepth 1 -type d -name '*istio*' -exec sudo rm -f -- {} \;
done

echo "Testing Done"

if [[ ${#FAILED_PACKAGES[@]} -gt 0 ]]; then
  echo ""
  echo "Some of the tests have failed :("
  echo ""
  echo "Packages with failed tests:"
  for pkg in "${FAILED_PACKAGES[@]}"; do
    echo "- ${pkg}"
  done
  exit 1
fi
