#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8-$VER"
az aks create --resource-group getistio --name $CLUSTER_NAME --node-count 2 --generate-ssh-keys --kubernetes-version $VER # change the resource group
az aks get-credentials --resource-group getistio --name $CLUSTER_NAME # change the resource group