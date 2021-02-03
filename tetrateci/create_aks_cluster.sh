#!/bin/bash
set -o errexit
set -o pipefail
SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $VER)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"
az aks create --resource-group $RESOURCE --name $CLUSTER_NAME --node-count 2 --generate-ssh-keys --kubernetes-version $VER -s standard_d8s_v3 --network-plugin azure
az aks get-credentials --resource-group $RESOURCE --name $CLUSTER_NAME