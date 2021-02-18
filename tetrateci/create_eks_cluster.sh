#!/usr/bin/env bash

set -o errexit
set -o pipefail

# istio 1.9 is not supported for k8s 1.16
[ $MINOR_VER == "1.9" ] && grep -q "1.16" <<< ${VER} && exit

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
    echo "warn: eksctl is not found in the \$path. downloading eksctl"
    curl --silent --location "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_$(uname -s)_amd64.tar.gz" | tar xz -C /tmp
    sudo mv /tmp/eksctl /usr/local/bin
fi

SHA8=$(git rev-parse --short $GITHUB_SHA)
SUFFIX=$(sed 's/\.//g' <<< $VER)
CLUSTER_NAME="test-istio-$SHA8-$SUFFIX"

echo "creating a eks cluster with \"$CLUSTER_NAME\" name..."
eksctl create cluster --name $CLUSTER_NAME --version $VER --nodes 3 --node-type m5.xlarge
