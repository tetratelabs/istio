#!/bin/bash

CLUSTER_NAME="test-istio-$GITHUB_SHA"
gcloud container clusters create $CLUSTER_NAME --machine-type "e2-standard-2" --num-nodes 3