#!/bin/bash

# HACK : the github runner runs out of space sometimes so removing the 21 GB dotnet folder
# Temporary thing, we should be moving to a custom runner instead.
[ -d "/usr/share/dotnet" ] && sudo rm -rf /usr/share/dotnet

sudo gem install fpm
sudo apt-get install go-bindata -y
cd ..
git clone https://github.com/istio/release-builder --depth=1
envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.yaml
cd release-builder
cp -r ../istio .
export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82
mkdir /tmp/istio-release
go run main.go build --manifest manifest.yaml
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated
go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB

PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio*")
for package in $PACKAGES; do
    echo "Publishing $package"
    curl -T /tmp/istio-release/out/$package -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/$package
done

curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/publish
