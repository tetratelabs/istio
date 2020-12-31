#!/bin/bash
export CLUSTER_NAME="test-istio-$GITHUB_SHA"
az aks create --resource-group getistio --name $CLUSTER_NAME --node-count 2 --generate-ssh-keys