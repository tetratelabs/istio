#!/usr/bin/env bash

set -o errexit
set -o pipefail

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
SUFFIX=$(sed 's/\.//g' <<< $VER)

## Cluster name has to end with k8s.local
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX.k8s.local"

## TODO: Change to appropriate directory
git clone https://github.com/aws/eks-distro.git
cd eks-distro/development/kops

export KOPS_STATE_STORE=s3://getistio-eksd-state-store
export KOPS_CLUSTER_NAME=${CLUSTER_NAME}

##TODO: make sure template file is copied from tetrateci
cp ../../../../eks-d.tpl .

##TODO: use AWS REGION from secret

##TODO: set RELEASE_BRANCH to the kubernetes version being used
export RELEASE_BRANCH=1.19


echo "creating a eksd cluster with \"$CLUSTER_NAME\" name..."
./run_cluster.sh

#Wait for the cluster to be created
#TODO - wait of 6minutes may not be enough. Loop this call twice.
./cluster_wait.sh

