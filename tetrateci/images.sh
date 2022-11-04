#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"

mkdir containers.istio.tetratelabs.com

IMAGES=(app
install-cni
istioctl
pilot
proxyv2)


IMAGE_SUFFIXES=("debug" "distroless")

for image in "${IMAGES[@]}"; do
  for suffix in "${IMAGE_SUFFIXES[@]}"; do
    echo containers.istio.tetratelabs.com/${image}:${TAG}-${suffix} >> list.txt
    cat list.txt
  done
done
