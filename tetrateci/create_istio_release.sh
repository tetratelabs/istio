#!/bin/bash

# HACK : the github runner runs out of space sometimes so removing the 21 GB dotnet folder
# Temporary thing, we should be moving to a custom runner instead.
echo "Trying to delete that massive dotnet folder"
[ -d "/usr/share/dotnet" ] && sudo rm -rf /usr/share/dotnet

sudo gem install fpm
sudo apt-get install go-bindata -y
cd ..
git clone https://github.com/istio/release-builder --depth=1
envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.yaml
cd release-builder
cp -r ../istio .
#export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82

ORIGINAL_TAG=$(cut -d"-" -f1 <<< $TAG)
echo "Original Tag $ORIGINAL_TAG"

echo "Downloading https://github.com/istio/api"
git clone --depth 1 --branch $ORIGINAL_TAG https://github.com/istio/api istio-api
cd istio-api && git tag $TAG && cd ..

echo "Downloading https://github.com/istio/pkg"
git clone --depth 1 --branch $ORIGINAL_TAG https://github.com/istio/pkg istio-pkg
cd istio-pkg && git tag $TAG && cd ..

echo "Downloading https://github.com/istio/client-go"
git clone --depth 1 --branch $ORIGINAL_TAG https://github.com/istio/client-go istio-client-go
cd istio-client-go && git tag $TAG && cd ..

echo "Downloading https://github.com/istio/gogo-genproto"
git clone --depth 1 --branch $ORIGINAL_TAG https://github.com/istio/gogo-genproto istio-gogo-genproto
cd istio-gogo-genproto && git tag $TAG && cd ..

echo "Downloading https://github.com/istio/test-infra"
git clone --depth 1 --branch $ORIGINAL_TAG https://github.com/istio/test-infra istio-test-infra
cd istio-test-infra && git tag $TAG && cd ..

echo "Downloading https://github.com/istio/tools"
git clone --depth 1 --branch $ORIGINAL_TAG https://github.com/istio/tools istio-tools
cd istio-tools && git tag $TAG && cd ..

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
