#!/bin/bash
echo "Deleting eks cluster $CLUSTER_NAME"
eksctl delete cluster --name $CLUSTER_NAME