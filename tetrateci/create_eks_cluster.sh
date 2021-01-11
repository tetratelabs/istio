#!/bin/bash

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

if ! command -v eksctl &> /dev/null
then
    echo "error: eksctl is not found in the \$path. downloading eksctl"
    curl --silent --location "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_$(uname -s)_amd64.tar.gz" | tar xz -C /tmp
    sudo mv /tmp/eksctl /usr/local/bin
fi

CLUSTER_NAME="test-istio-$GITHUB_SHA"

echo "creating a eks cluster with \"$CLUSTER_NAME\" name..."
eksctl create cluster --name $CLUSTER_NAME --version 1.18 