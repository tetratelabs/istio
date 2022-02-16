#!/usr/bin/env bash
#
# Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

#
# Apply patches to the Istio code base that are necessary to fix e2e tests.
#
# E.g., after we bumped version of Go from `1.16` to `1.17`, e2e tests of
# `Istio 1.11` started failing.
#
# To fix e2e tests, we had to backport changes from `Istio 1.12`.
#
# However, since required changes affected only test code and test images,
# we didn't want to include them into the release build.
#

set -e
set -u
set -x

SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

while IFS= read -r -d '' patch
do
    git apply "${patch}"
done < <(find "${SCRIPTDIR}/patches/build/e2e/${REL_BRANCH_VER}" -type f -name '*.patch' -print0)
