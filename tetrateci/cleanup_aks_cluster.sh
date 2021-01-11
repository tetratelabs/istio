#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8"
az aks delete --name $CLUSTER_NAME --resource-group getistio --yes # change the resource group