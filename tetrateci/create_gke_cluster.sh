#!/bin/bash

CLUSTER_NAME="test-istio-$GITHUB_SHA"
gcloud container clusters create $CLUSTER_NAME --machine-type "e2-standard-2" --num-nodes 3 --region=us-central1-c
gcloud config set container/use_client_certificate False
gcloud container clusters get-credentials $CLUSTER_NAME --region us-central1-c
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="$(gcloud config get-value core/account)"
gcloud container clusters update $CLUSTER_NAME --update-addons=NetworkPolicy=ENABLED --region us-central1-c
gcloud container clusters update $CLUSTER_NAME --enable-network-policy --region us-central1-c