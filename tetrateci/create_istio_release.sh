#!/bin/bash

set -o errexit
set -o pipefail

# HACK : the github runner runs out of space sometimes so removing the 21 GB dotnet folder
# Temporary thing, we should be moving to a custom runner instead.
echo "Deleting /usr/share/dotnet to reclaim space"
[ -d "/usr/share/dotnet" ] && sudo rm -rf /usr/share/dotnet
echo "Deletetion complete"

if [[ ${BUILD} == "fips" ]]; then
    export GOLANG_VERSION=1.15.7b5
    echo "Fetching FIPS compliant Go"
    url="https://go-boringcrypto.storage.googleapis.com/go1.15.7b5.linux-amd64.tar.gz"
    wget -O go.tgz "$url"
    echo "cb08962897e3802cda96f4ee915ed20fbde7d5d85e688759ef523d2e6ae44851 go.tgz" | sha256sum -c -
    sudo tar -C /usr/local -xzf go.tgz
    rm go.tgz
    export GOROOT=/usr/local/go
    export PATH="$GOROOT/bin:$PATH"
    echo "FIPS compliant Go installed"
    go version
fi

export ISTIO_VERSION=$TAG

sudo gem install fpm
sudo apt-get install go-bindata -y
cd ..
git clone https://github.com/istio/release-builder --depth=1
envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.yaml
cd release-builder
cp -r ../istio .
# export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82
mkdir /tmp/istio-release
go run main.go build --manifest manifest.yaml
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated
go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB

PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio*")
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
