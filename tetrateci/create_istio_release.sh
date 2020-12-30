#!/bin/bash

mkdir /tmp/istio-release
git clone https://github.com/istio/release-builder --depth=1
cd release-builder
go run main.go build --manifest ../ci/manifest.yaml