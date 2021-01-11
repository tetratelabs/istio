#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8"
echo "Deleting eks cluster $CLUSTER_NAME"
eksctl delete cluster --name $CLUSTER_NAME