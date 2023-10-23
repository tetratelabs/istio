#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"


IMAGES=(install-cni
istioctl
operator
pilot
proxyv2)

IMAGE_SUFFIXES=("" "-debug" "-distroless")

for image in "${IMAGES[@]}"; do
  for suffix in "${IMAGE_SUFFIXES[@]}"; do
    docker tag $HUB/${image}:${TAG}${suffix} $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}
    echo $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}
    docker push $ECR_REGISTRY/tid-istio/${image}:${TAG}${suffix}
  done
done

//do not copy images to public registry if backport=true
if [[ ${BACKPORT} == "false" ]] ; then
    for image in "${IMAGES[@]}"; do
        for suffix in "${IMAGE_SUFFIXES[@]}"; do
            docker tag $HUB/${image}:${TAG}${suffix} $PUBLIC_HUB/${image}:${TAG}${suffix}
            echo $PUBLIC_HUB/${image}:${TAG}${suffix}
            docker push $PUBLIC_HUB/${image}:${TAG}${suffix}
        done
    done
fi
