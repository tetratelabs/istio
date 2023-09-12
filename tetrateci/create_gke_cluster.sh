#!/usr/bin/env bash

./tetrateci/version_check.py && exit

set -o errexit
set -o pipefail

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $K8S_VERSION)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"
export USE_GKE_GCLOUD_AUTH_PLUGIN=True
gcloud components install gke-gcloud-auth-plugin
gcloud container clusters create $CLUSTER_NAME --machine-type "n1-standard-4" --num-nodes 3 --region=us-central1-c --enable-network-policy --cluster-version $K8S_VERSION --release-channel "$CHAN"
gcloud container clusters get-credentials $CLUSTER_NAME --region us-central1-c
