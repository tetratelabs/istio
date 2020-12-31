#!/bin/bash
export CLUSTER_NAME="test-istio-$GITHUB_SHA"
export RESOURCE_NAME="resource-istio-$GITHUB_SHA"
az group create --name $RESOURCE_NAME --location eastus
az aks create --resource-group $RESOURCE_NAME --name $CLUSTER_NAME --node-count 2 --generate-ssh-keys
az aks get-credentials --resource-group $RESOURCE_NAME --name $CLUSTER_NAME