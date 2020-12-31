#!/bin/bash
CLUSTER_NAME="test-istio-$GITHUB_SHA"
gcloud container clusters delete $CLUSTER_NAME