#!/usr/bin/env bash
set -e
set -u

if $(grep -q "1.7" <<< $TAG); then
    export GOLANG_VERSION=1.14.12
fi

if $(grep -q "1.8" <<< $TAG || grep -q "1.9" <<< $TAG); then
    export GOLANG_VERSION=1.15.7
fi

if $(grep -q "1.10" <<< $TAG || grep -q "1.11" <<< $TAG); then
    export GOLANG_VERSION=1.16.9
fi

if [[ "${REL_BRANCH_VER:-${ISTIO_MINOR_VER}}" == "1.12" ]]; then
    export GOLANG_VERSION=1.17.3
fi

url="https://golang.org/dl/go$GOLANG_VERSION.linux-$(dpkg --print-architecture).tar.gz"

wget -q -O go.tgz "$url"

sudo tar -C /usr/local -xzf go.tgz
rm go.tgz

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"

echo "Go installed"
go version
