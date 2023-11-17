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

# TG=(1.19.3-tetrate-v1
# 1.19.4-tetrate-v0
# 1.17.8-tetratefips-v1
# 1.18.5-tetrate-v2
# 1.18.5-tetrate-v3
# 1.16.7-tetratefips-v3)

TG1=(1.19.3-tetrate-v1
1.19.4-tetrate-v0
1.17.8-tetrate-v1
1.18.5-tetrate-v2
1.18.5-tetrate-v3)



# HB=fips-containers.istio.tetratelabs.com


IMAGE_SUFFIXES=("debug" "distroless")

# for image in "${IMAGES[@]}"; do
#   for suffix in "${IMAGE_SUFFIXES[@]}"; do
#     DIGEST=$(crane digest $HB/${image}:${TG}-${suffix})
#     cosign sign -y --identity-token=$(gcloud auth print-identity-token --audiences=sigstore --include-email --impersonate-service-account image-signing-keyless-sa@tid-testing.iam.gserviceaccount.com) $HB/${image}@$DIGEST
#   done
# done


HB1=containers.istio.tetratelabs.com
for image in "${IMAGES[@]}"; do
    for suffix in "${IMAGE_SUFFIXES[@]}"; do
        for tag in "${TG1[@]}"; do 
            DIGEST=$(crane digest $HB1/${image}:${tag}-${suffix})
            echo "Signing $HB1/${image}:${tag}-${suffix}"
            cosign sign -y --identity-token=$(gcloud auth print-identity-token --audiences=sigstore --include-email --impersonate-service-account image-signing-keyless-sa@tid-testing.iam.gserviceaccount.com) $HB1/${image}@$DIGEST
        done    
    done
done
