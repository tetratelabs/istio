#!/bin/bash

SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8"
gcloud container clusters create $CLUSTER_NAME --machine-type "e2-standard-2" --num-nodes 3 --region=us-central1-c --enable-network-policy
gcloud config set container/use_client_certificate False
gcloud container clusters get-credentials $CLUSTER_NAME --region us-central1-c
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="$(gcloud config get-value core/account)"