#!/bin/bash

set -o errexit
set -o pipefail

# HACK : the github runner runs out of space sometimes so removing the 21 GB dotnet folder
# Temporary thing, we should be moving to a custom runner instead.
echo "Deleting /usr/share/dotnet to reclaim space"
[ -d "/usr/share/dotnet" ] && sudo rm -rf /usr/share/dotnet
echo "Deletetion complete"

if [[ ${BUILD} == "fips" ]]; then
    ./tetrateci/setup_boring_go.sh
    export ISTIO_ENVOY_WASM_BASE_URL=https://storage.googleapis.com/istio-build/proxy 
    export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/getistio-build/proxy-fips
fi

# if length $TEST is zero we are making a release
if [[ -z TEST ]]; then
    # since we are building the final release
    echo "  - archive" >> tetrateci/manifest.yaml.in
fi

export ISTIO_VERSION=$TAG

sudo gem install fpm
sudo apt-get install go-bindata -y
envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.yaml
cd ..
git clone https://github.com/istio/release-builder --depth=1
cd release-builder
cp -r ../istio .
# export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82
mkdir /tmp/istio-release
go run main.go build --manifest manifest.yaml
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated
go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB

if [[ ${BUILD} != "fips" ]]; then
    PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio")
else
    PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio" | grep "linux-amd64")
fi

if [[ -z TEST ]]; then
    for package in $PACKAGES; do
        echo "Publishing $package"
        rm -f /tmp/curl.out
        curl -T /tmp/istio-release/out/$package -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/$package -o /tmp/curl.out
        cat /tmp/curl.out
        grep "success" /tmp/curl.out
    done

    rm -f /tmp/curl.out
    curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/publish -o /tmp/curl.out
    cat /tmp/curl.out
fi