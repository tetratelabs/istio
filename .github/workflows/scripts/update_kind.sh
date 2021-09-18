#!/usr/bin/env bash

set -e

# Install the latest version of KinD to fix https://github.com/kubernetes-sigs/kind/issues/2240
curl -o /tmp/kind -L https://github.com/kubernetes-sigs/kind/releases/download/v0.11.1/kind-linux-amd64
chmod a+x /tmp/kind
mv /tmp/kind /gobin/kind
kind version

# execute the original entrypoint
exec /usr/local/bin/docker-entrypoint "$@"
