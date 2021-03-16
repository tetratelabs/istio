#!/usr/bin/env bash

./tetrateci/version_check.py && exit

set -o errexit
set -o pipefail

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $K8S_VERSION)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"
gcloud container clusters delete $CLUSTER_NAME --region us-central1-c --quiet
