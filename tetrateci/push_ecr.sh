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
    docker tag $HUB/${image}:${TAG}${suffix} 957006768579.dkr.ecr.us-east-2.amazonaws.com/tid-istio/${image}:${TAG}${suffix}
    docker push 957006768579.dkr.ecr.us-east-2.amazonaws.com/tid-istio/${image}:${TAG}${suffix}
  done
done
