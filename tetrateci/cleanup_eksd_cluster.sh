#!/usr/bin/env bash

set -o errexit
set -o pipefail


BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"

if [[ ! -f ~/.aws/config && ! -f ~/.aws/credentials ]]
then
    echo "warn: didn't find config and credentials in ~/.aws."
    echo "checking for environment varibles...."
    if [[ ! -v AWS_ACCESS_KEY_ID && ! -v AWS_SECRET_ACCESS_KEY ]]
    then
        echo "error: neither is aws_access_key_id and aws_secret_access_key is set."
        exit 2
    fi
fi

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $K8S_VERSION)

## Cluster name has to end with k8s.local
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX.k8s.local"

cd $BASEDIR/../eks-distro/development/kops

export KOPS_STATE_STORE=s3://${S3_BUCKET}
export KOPS_CLUSTER_NAME=${CLUSTER_NAME}


#Wait for the cluster to be created
./delete_cluster.sh

