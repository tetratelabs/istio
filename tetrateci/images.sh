#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"



IMAGES=(install-cni
proxyv2
operator
pilot)

IMAGE_SUFFIXES=("debug" "distroless")

for image in "${IMAGES[@]}"; do
  for suffix in "${IMAGE_SUFFIXES[@]}"; do
    DIGEST=$(crane digest $HUB/${image}:${TAG}-${suffix})
    cosign sign -y --identity-token=$(gcloud auth print-identity-token --audiences=sigstore --include-email --impersonate-service-account image-signing-keyless-sa@tid-testing.iam.gserviceaccount.com) $HUB/${image}@$DIGEST
  done
done
