#!/bin/bash
CLUSTER_NAME="test-istio-$GITHUB_SHA"
echo "Deleting eks cluster $CLUSTER_NAME"
eksctl delete cluster --name $CLUSTER_NAME