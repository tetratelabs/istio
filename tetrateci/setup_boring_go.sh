#!/usr/bin/env bash
set -e
set -u

if $(grep -q "1.7" <<< $TAG); then
  export GOLANG_VERSION=1.14.12b4
fi

if $(grep -q "1.8" <<< $TAG || grep -q "1.9" <<< $TAG); then
  export GOLANG_VERSION=1.15.8b5
fi

if $(grep -q "1.10" <<< $TAG); then
  export GOLANG_VERSION=1.16.9b7
fi

if $(grep -q "1.11" <<< $TAG || grep -q "1.12" <<< $TAG); then
  export GOLANG_VERSION=1.17.6b7
fi

if [[ "${REL_BRANCH_VER:-${ISTIO_MINOR_VER}}" == "1.12" ]]; then
  export GOLANG_VERSION=1.17.8b7
fi

url="https://go-boringcrypto.storage.googleapis.com/go$GOLANG_VERSION.linux-amd64.tar.gz"

wget -q -O go.tgz "$url"

sudo tar -C /usr/local -xzf go.tgz
rm go.tgz

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"

echo "FIPS compliant Go installed"
go version
