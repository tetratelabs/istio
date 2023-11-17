#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"



IMAGES=(install-cni
proxyv2
operator
istioctl
pilot)






TG1=(1.16.7-tetratefips-v3)





IMAGE_SUFFIXES=("debug" "distroless")


for image in "${IMAGES[@]}"; do
    for suffix in "${IMAGE_SUFFIXES[@]}"; do
        for tag in "${TG1[@]}"; do 
            docker pull $HUB/${image}:${tag}-${suffix}
            DIGEST=$(crane digest $HUB/${image}:${tag}-${suffix})
            echo "Signing $HUB/${image}:${tag}-${suffix}"
            cosign sign -y --identity-token=$(gcloud auth print-identity-token --audiences=sigstore --include-email --impersonate-service-account image-signing-keyless-sa@tid-testing.iam.gserviceaccount.com) $HUB/${image}@$DIGEST
        done    
    done
done