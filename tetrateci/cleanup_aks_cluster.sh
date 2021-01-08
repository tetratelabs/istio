#!/bin/bash
CLUSTER_NAME="test-istio-$GITHUB_SHA"
az aks delete --name $CLUSTER_NAME --resource-group getistio --yes # change the resource group