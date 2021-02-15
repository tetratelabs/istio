#!env bash
set -e

echo "Fetching FIPS compliant Go"
url="https://golang.org/dl/go$GOLANG_VERSION.linux-amd64.tar.gz"

wget -O go.tgz "$url"

sudo tar -C /usr/local -xzf go.tgz
rm go.tgz

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"

echo "Go installed"
go version