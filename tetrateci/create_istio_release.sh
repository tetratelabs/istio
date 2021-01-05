#!/bin/bash

mkdir /tmp/istio-release

sudo gem install fpm
sudo apt-get install go-bindata -y
envsubst < ./tetrateci/manifest.yaml.in > manifest.yaml
git clone https://github.com/istio/release-builder --depth=1
cd release-builder
go run main.go build --manifest ../manifest.yaml
go run main.go publish --release /tmp/istio-release/out --dockerhub hellozee-docker-istio-temp.bintray.io