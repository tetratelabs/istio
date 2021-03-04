#!/usr/bin/env bash

# If fips build is enabled then we switch to boring go for the docker images but use the default 
# go for building the istioctl binary

set -o errexit
set -o pipefail

# HACK : the github runner runs out of space sometimes so removing the 21 GB dotnet folder
# Temporary thing, we should be moving to a custom runner instead.
echo "Deleting /usr/share/dotnet to reclaim space"
[ -d "/usr/share/dotnet" ] && sudo rm -rf /usr/share/dotnet
echo "Deletetion complete"

# The test flag is to check whether we are building images for testing or release
# in case of release we build the istioctl too which we don't need in case of testing.
echo "TEST flag is '$TEST'"

if [[ ${BUILD} == "fips" ]]; then
    sudo -E ./tetrateci/setup_boring_go.sh
    export ISTIO_ENVOY_WASM_BASE_URL=https://storage.googleapis.com/istio-build/proxy 
    export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/getistio-build/proxy-fips
else
    sudo -E ./tetrateci/setup_go.sh
fi

# HACK : change the distroless base image to include glibc
# We would use the same distroless base image as istio-proxy
# for pilot and operator
if [[ ${BUILD} == "fips" ]]; then
	PROXY_DISTROLESS_BASE=$(grep 'as distroless' pilot/docker/Dockerfile.proxyv2)
	# Escape '/'
	PROXY_DISTROLESS_BASE_ESCAPED=$(sed 's/\//\\\//g' <<< ${PROXY_DISTROLESS_BASE})
	sed -i "s/.*as distroless/${PROXY_DISTROLESS_BASE_ESCAPED}/" pilot/docker/Dockerfile.pilot
	sed "s/.*as distroless/${PROXY_DISTROLESS_BASE_ESCAPED}/" operator/docker/Dockerfile.operator
fi

# HACK : This is needed during istio build for istiod to serve version command
export ISTIO_VERSION=$TAG

sudo gem install fpm
sudo apt-get install go-bindata -y
export BRANCH=release-${REL_BRANCH_VER}
cd ..
git clone https://github.com/istio/release-builder --branch ${BRANCH}

# HACK : default manifest from release builder is modified
echo "Generating the docker manifest"
envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.docker.yaml
echo "  - docker" >> ./release-builder/manifest.docker.yaml

# if length $TEST is zero we are making a release
if [[ -z $TEST ]]; then
    # since we are building the final release
    echo "Generating the archive manifest"
    envsubst < ./istio/tetrateci/manifest.yaml.in > ./release-builder/manifest.archive.yaml
    echo "  - archive" >> ./release-builder/manifest.archive.yaml
fi

echo "Getting into release builder"
cd release-builder

echo "Copying istio directory"
cp -r ../istio .
# export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82
mkdir /tmp/istio-release
go run main.go build --manifest manifest.docker.yaml
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated
go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB

if [[ -z $TEST ]]; then
    echo "Starting archive build"
    echo "Cleaning up the docker build...."

    [ -d "/tmp/istio-release" ] && sudo rm -rf /tmp/istio-release

    mkdir /tmp/istio-release

    echo "Building archives..."
    go run main.go build --manifest manifest.archive.yaml

    PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio")

    for package in $PACKAGES; do
        echo "Publishing $package"
        rm -f /tmp/curl.out
        curl -T /tmp/istio-release/out/$package -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/$package -o /tmp/curl.out
        cat /tmp/curl.out
        grep "success" /tmp/curl.out
    done

    rm -f /tmp/curl.out
    curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/publish -o /tmp/curl.out
    cat /tmp/curl.out
fi

echo "Done building and pushing the artifacts."
