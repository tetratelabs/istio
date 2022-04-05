#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"

## Set up apporiate go version
if [[ ${TAG} =~ "fips" ]]; then
    echo "Set up FIPS compliant Golang"
    source ${BASEDIR}/tetrateci/setup_boring_go.sh
else
    echo "Set up Golang"
    source ${BASEDIR}/tetrateci/setup_go.sh
fi

## Set up release-builder

# BOM is needed for generating bill of materials, required by Istio since 1.13, https://github.com/istio/release-builder/pull/893
go install sigs.k8s.io/bom/cmd/bom@v0.2.2
cp /home/runner/go/bin/bom /usr/local/bin/

sudo gem install fpm
sudo apt-get install go-bindata -y
export BRANCH=release-${REL_BRANCH_VER}
cd ..
git clone https://github.com/istio/release-builder --branch ${BRANCH}


# HACK : the github runner runs provides 14 GB free space. (https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners#supported-runners-and-hardware-resources).
# Temporary thing, we should be moving to a custom runner instead.
echo "Deleting /usr/share/dotnet, /opt/ghc, /usr/local/share/boost to reclaim space"
for i in /usr/share/dotnet /opt/ghc /usr/local/share/boost; do echo deleting folder $i; [ -d $i ] && rm -rf "$i" ; done
echo "Deletion complete"

# HACK : This is needed during istio build for istiod to serve version command
export ISTIO_VERSION=$TAG

# We are not using a docker container to build the istioctl binary and images, so we make it explicit
export BUILD_WITH_CONTAINER=0

# HACK : For FIPS change the distroless base image to include glibc
# We would use the same distroless base image as istio-proxy for pilot and operator
# HACK : change envoy/wasm base URL to point to FIPS compliant one
if [[ ${TAG} =~ "fips" ]]; then
	PROXY_DISTROLESS_BASE=$(grep 'as distroless' ${BASEDIR}/pilot/docker/Dockerfile.proxyv2)
	# Escape '/'
	PROXY_DISTROLESS_BASE_ESCAPED=$(sed 's/\//\\\//g' <<< ${PROXY_DISTROLESS_BASE})
	sed -i "s/.*as distroless/${PROXY_DISTROLESS_BASE_ESCAPED}/" ${BASEDIR}/pilot/docker/Dockerfile.pilot
	sed -i "s/.*as distroless/${PROXY_DISTROLESS_BASE_ESCAPED}/" ${BASEDIR}/operator/docker/Dockerfile.operator

    export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/getistio-build/proxy-fips
fi

# HACK : default manifest from release builder is modified
echo "Generating the manifests"
# we are generating the different yamls for both the archive & docker image builds which are saved to release-builder folder
python3 -m pip install pyyaml --user
${BASEDIR}/tetrateci/gen_release_manifest.py ${BASEDIR}/../release-builder/example/manifest.yaml ${BASEDIR}/../release-builder/

# if length $TEST is zero we are making a RELEASE. It should have both images and archives
# The test flag is to check whether we are building images for testing or release
# in case of release we build the istioctl too which we don't need in case of testing.
echo "TEST flag is '${TEST:-}'"

echo "Getting into release builder"
cd release-builder
echo "Copying istio directory"
cp -r ../istio .
# export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82

# "Enabling CGO for FIPS build via CGO_ENABLED=1"
echo "Enabling CGO for FIPS build via CGO_ENABLED=1 to istio/common/scripts/gobuild.sh"

if [[ ${TAG} =~ "fips" ]]; then
  echo "Checking if the upstream file is not changed"
  if ! grep -q 'CGO_ENABLED=${CGO_ENABLED:-0}' istio/common/scripts/gobuild.sh;then exit 1;fi
  text="if [[ "\${GOARCH}" == "amd64" ]]; then export CGO_ENABLED=1; else export CGO_ENABLED=0; fi"
  sed -i 's/export CGO_ENABLED=${CGO_ENABLED:-0}/'"$text"'/g' istio/common/scripts/gobuild.sh
fi
# Build Docker Images
mkdir /tmp/istio-release
go run main.go build --manifest manifest.docker.yaml
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated

#loading pilot image manually since docker container create command is failing due to unavailbilty of pilot image locally
docker load -i /tmp/istio-release/out/docker/pilot.tar.gz

CONTAINER_ID=$(docker create $HUB/pilot:$TAG)
docker cp $CONTAINER_ID:/usr/local/bin/pilot-discovery pilot-bin
# go version with which the binaries for the docker images wi
BUILD_GO_VERSION=$(go version pilot-bin | cut -f2 -d" ")
echo "Images are built with: go $BUILD_GO_VERSION"

[ $BUILD_GO_VERSION == go$GOLANG_VERSION ] || exit 1

# fips go versions are like 1.14.12b5, extra checking to not miss anything
if [ ${TAG} =~ "fips" ]; then 
    [[ $BUILD_GO_VERSION =~ 1.[0-9]+.[0-9]+[a-z][0-9]$ ]] || exit 1
fi

go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB
echo "Cleaning up the istio source artificats...."
sudo rm -rf /tmp/istio-release/sources/

# If RELEASE, Build Archives
if [[ -z ${TEST:-} ]]; then
    echo "Building archives..."
    # if FIPS, need to use native go as boringgo as of now can't build archives for different platforms
    if [[ ${TAG} =~ "fips" ]]; then
        sudo rm -rf /usr/local/go
        source ${BASEDIR}/tetrateci/setup_go.sh
        #disabling cgo flag
        sed -i '/then export CGO_ENABLED=1/c\export CGO_ENABLED=0' istio/common/scripts/gobuild.sh
    fi
    echo "Cleaning up older artifacts created in docker build stage ..."
    sudo rm -rf /tmp/istio-release/sources/ && sudo rm -rf /tmp/istio-release/work/
    go run main.go build --manifest manifest.archive.yaml

    python3 -m pip install --upgrade cloudsmith-cli --user

    PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio")
    for package in $PACKAGES; do
        echo "Publishing $package"
        cloudsmith push raw tetrate/getistio /tmp/istio-release/out/$package
    done
fi
echo "Cleaning /tmp/istio...."
[ -d "/tmp/istio-release" ] && sudo rm -rf /tmp/istio-release

echo "Done building and pushing the artifacts."
