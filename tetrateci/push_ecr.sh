#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"


IMAGES=(install-cni
istioctl
operator
pilot
proxyv2
ztunnel)

IMAGE_SUFFIXES=("" "-debug" "-distroless")

for image in "${IMAGES[@]}"; do
  for suffix in "${IMAGE_SUFFIXES[@]}"; do
    echo ${HUB}/${image}:${TAG}${suffix}
    crane copy ${HUB}/${image}:${TAG}${suffix} $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}

    # docker tag $HUB/${image}:${TAG}${suffix} $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}
    # echo $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}
    # docker push $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}
  done
done

# do not copy if it is a FIPS build or backport.
if  [[ ${TAG} =~ "fips" ]] ;then
  exit 0;
elif [[ ${BACKPORT} == "false" ]]  ; then
    for image in "${IMAGES[@]}"; do
        for suffix in "${IMAGE_SUFFIXES[@]}"; do
            # docker tag $HUB/${image}:${TAG}${suffix} $PUBLIC_HUB/${image}:${TAG}${suffix}
            # echo $PUBLIC_HUB/${image}:${TAG}${suffix}
            # docker push $PUBLIC_HUB/${image}:${TAG}${suffix}
            echo ${HUB}/${image}:${TAG}${suffix}
            crane copy ${HUB}/${image}:${TAG}${suffix} $PUBLIC_HUB/${image}:${TAG}${suffix}
        done
    done
fi