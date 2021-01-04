#!/bin/bash
SHA8=$(git rev-parse --short $GITHUB_SHA)
CLUSTER_NAME="test-istio-$SHA8"
gcloud container clusters delete $CLUSTER_NAME --region us-central1-c --quiet
