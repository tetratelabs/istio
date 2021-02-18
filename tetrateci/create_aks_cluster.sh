#!/usr/bin/env bash
set -o errexit
set -o pipefail

# istio 1.9 is not supported for k8s 1.16
[ $MINOR_VER == "1.9" ] && grep -q "1.16" <<< ${VER} && exit

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $VER)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"

echo "Fetching location of the resource"
location=$(az group show -g $RESOURCE | jq '.location')

echo "Fetching available kubernetes patch version for $VER"
version=$(az aks get-versions -l $location | jq '.orchestrators[] | .orchestratorVersion' | grep $VER | tail -n 1)

echo "Kubernetes version selected: $version"

az aks create --resource-group $RESOURCE --name $CLUSTER_NAME --node-count 3 --generate-ssh-keys --kubernetes-version $version -s standard_ds3_v2 --network-plugin azure
az aks get-credentials --resource-group $RESOURCE --name $CLUSTER_NAME
