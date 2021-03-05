#!/usr/bin/env bash

./tetrateci/version_check.py && exit

set -o errexit
set -o pipefail

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $K8S_VERSION)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"

echo "Fetching location of the resource"
location=$(az group show -g $RESOURCE | jq '.location')

echo "Fetching available kubernetes patch version for $K8S_VERSION"
version=$(az aks get-versions -l $location | jq '.orchestrators[] | .orchestratorVersion' | grep $K8S_VERSION | tail -n 1)

echo "Kubernetes version selected: $version"

az aks create --resource-group $RESOURCE --name $CLUSTER_NAME --node-count 3 --generate-ssh-keys --kubernetes-version $version -s standard_ds3_v2 --network-plugin azure
az aks get-credentials --resource-group $RESOURCE --name $CLUSTER_NAME
