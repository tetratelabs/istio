#!/usr/bin/env bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $VER)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"
az aks delete --name $CLUSTER_NAME --resource-group $RESOURCE --yes || true
