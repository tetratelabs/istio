#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $VER)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"
gcloud container clusters delete $CLUSTER_NAME --region us-central1-c --quiet
