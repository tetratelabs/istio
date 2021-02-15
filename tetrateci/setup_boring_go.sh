#!/bin/bash

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