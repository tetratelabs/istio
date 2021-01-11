#!/bin/bash
cd ..
git clone https://github.com/istio/release-builder --depth=1
envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.yaml
cd release-builder
cp -r ../istio .
export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
make shell
mkdir /tmp/istio-release
go run main.go build --manifest manifest.yaml
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated
#go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB

#PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio*")
#for package in $PACKAGES; do
#    NAME=$(cut -d '-' -f 1 <<< $package)
#    echo "Publishing $package"
#    curl -T /tmp/istio-release/out/$package -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$NAME/$TAG/$package 
#done

#curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/istio/$TAG/publish
#curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/istioctl/$TAG/publish
