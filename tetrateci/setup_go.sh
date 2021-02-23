#!/usr/bin/env bash
set -e

if $(grep -q "1.7" <<< $TAG); then
    export GOLANG_VERSION=1.14.12
fi

if $(grep -q "1.8" <<< $TAG || grep -q "1.9" <<< $TAG); then
    export GOLANG_VERSION=1.15.7
fi

echo "Fetching Go $GOLANG_VERSION"
url="https://golang.org/dl/go$GOLANG_VERSION.linux-amd64.tar.gz"

wget -O go.tgz "$url"

sudo tar -C /usr/local -xzf go.tgz
rm go.tgz

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"

echo "Go installed"
go version
