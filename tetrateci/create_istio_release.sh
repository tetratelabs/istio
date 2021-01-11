#!/bin/bash

mkdir /tmp/istio-release
sudo gem install fpm
sudo apt-get install go-bindata -y
git clone https://github.com/istio/release-builder --depth=1
envsubst < ./tetrateci/manifest.yaml.in > ./release-builder/manifest.yaml
cd release-builder
go run main.go build --manifest manifest.yaml
go run main.go validate --release /tmp/istio-release/out
go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB

PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio*")
for package in $PACKAGES; do
    NAME=$(cut -d '-' -f 1 <<< $package)
    echo "Publishing $package"
    curl -T /tmp/istio-release/out/$package -u$BINTRAY_USER:$API_KEY https://api.bintray.com/content/hellozee/istio-archives/$NAME/$TAG/$package #change
done

curl -X POST -u$BINTRAY_USER:$API_KEY https://api.bintray.com/content/hellozee/istio-archives/istio/$TAG/publish  #change
curl -X POST -u$BINTRAY_USER:$API_KEY https://api.bintray.com/content/hellozee/istio-archives/istioctl/$TAG/publish  #change