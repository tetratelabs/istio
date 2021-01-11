#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8"
az aks create --resource-group getistio --name $CLUSTER_NAME --node-count 2 --generate-ssh-keys # change the resource group
az aks get-credentials --resource-group getistio --name $CLUSTER_NAME # change the resource group