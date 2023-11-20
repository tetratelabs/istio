#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"



IMAGES=(install-cni
proxyv2
operator
istioctl
pilot
ztunnel)

IMAGE_SUFFIXES=("debug" "distroless")

for image in "${IMAGES[@]}"; do
  for suffix in "${IMAGE_SUFFIXES[@]}"; do
    DIGEST=$(crane digest $HUB/${image}:${TAG}-${suffix})
    echo "Signing $HUB/${image}:${TAG}-${suffix}"
    cosign sign -y --identity-token=$(gcloud auth print-identity-token --audiences=sigstore --include-email --impersonate-service-account image-signing-keyless-sa@tid-testing.iam.gserviceaccount.com) $HUB/${image}@$DIGEST
  done
done

if  [[ ${TAG} =~ "fips" ]] ;then
  exit 0;
elif [[ ${BACKPORT} == "false" ]] ; then
    for image in "${IMAGES[@]}"; do
        for suffix in "${IMAGE_SUFFIXES[@]}"; do
            echo "signing $PUBLIC_HUB/${image}:${TAG}-${suffix}"
            DIGEST=$(crane digest $PUBLIC_HUB/${image}:${TAG}-${suffix})
            cosign sign -y --identity-token=$(gcloud auth print-identity-token --audiences=sigstore --include-email --impersonate-service-account image-signing-keyless-sa@tid-testing.iam.gserviceaccount.com) $PUBLIC_HUB/${image}@$DIGEST
        done
    done
fi