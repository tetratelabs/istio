#!/bin/bash
set -o errexit
set -o pipefail

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $VER)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"
gcloud container clusters create $CLUSTER_NAME --machine-type "e2-standard-2" --num-nodes 3 --region=us-central1-c --enable-network-policy --cluster-version $VER --release-channel "regular"
gcloud config set container/use_client_certificate False
gcloud container clusters get-credentials $CLUSTER_NAME --region us-central1-c
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="$(gcloud config get-value core/account)"