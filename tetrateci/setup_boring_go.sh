#!env bash
set -e

if $(grep "1.7" <<< $TAG); then
  export GOLANG_VERSION=1.14.12b4
fi

if $(grep -q "1.8" <<< $TAG || grep -q "1.9" <<< $TAG); then
    export GOLANG_VERSION=1.15.8b5
fi

echo "Fetching FIPS compliant Go"
url="https://go-boringcrypto.storage.googleapis.com/go$GOLANG_VERSION.linux-amd64.tar.gz"

wget -O go.tgz "$url"

sudo tar -C /usr/local -xzf go.tgz
rm go.tgz

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"

echo "FIPS compliant Go installed"
go version
