#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8-$VER"
az aks delete --name $CLUSTER_NAME --resource-group $RESOURCE --yes