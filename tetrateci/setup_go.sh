#!env bash
set -e

echo "Fetching Go $GOLANG_VERSION"
url="https://golang.org/dl/go$GOLANG_VERSION.linux-amd64.tar.gz"

wget -O go.tgz "$url"

sudo tar -C /usr/local -xzf go.tgz
rm go.tgz

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"

echo "Go installed"
go version