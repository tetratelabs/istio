#!/usr/bin/env bash

curl -o /tmp/kind -L https://github.com/kubernetes-sigs/kind/releases/download/v0.11.1/kind-linux-amd64
chmod a+x /tmp/kind
mv /tmp/kind /gobin/kind
kind version

exec /usr/local/bin/docker-entrypoint "$@"
