#!/usr/bin/env bash

set -o errexit
set -o pipefail
# set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"

IMAGES=(app
app_sidecar_centos_7
app_sidecar_centos_8
app_sidecar_debian_10
app_sidecar_debian_9
app_sidecar_ubuntu_bionic
app_sidecar_ubuntu_focal
app_sidecar_ubuntu_xenial
install-cni
istioctl
operator
pilot
proxyv2)

IMAGE_SUFFIXES=("debug" "distroless")

for image in "${IMAGES[@]}"; do
  for suffix in "${IMAGE_SUFFIXES[@]}"; do
    echo $HUB/${image}:${TAG}-${suffix} >> list.txt
    cat list.txt
  done
done