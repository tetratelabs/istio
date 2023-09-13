#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -x

BASEDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"

sudo rm -rf /usr/local/go

source ${BASEDIR}/tetrateci/setup_go.sh



## Set up release-builder

# BOM is needed for generating bill of materials, required by Istio since 1.13, https://github.com/istio/release-builder/pull/893
# go install sigs.k8s.io/bom/cmd/bom@v0.2.2
# sudo cp /home/runner/go/bin/bom /usr/local/bin/

sudo gem install fpm
sudo apt-get install go-bindata -y
export BRANCH=release-${REL_BRANCH_VER}
cd ..
rm -rf release-builder
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
	sed -i "s/.*as distroless/${PROXY_DISTROLESS_BASE_ESCAPED}/" ${BASEDIR}/operator/docker/Dockerfile.operator
  sed -i "s/.*as distroless/${PROXY_DISTROLESS_BASE_ESCAPED}/" ${BASEDIR}/pilot/docker/Dockerfile.pilot
  export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/getistio-build/proxy-fips
fi


if [[ "$(uname -m)" = "aarch64" ]]; then
    sed -i 's/gcr\.io\/istio-release/gcr\.io\/tetrate-istio-arm/' $(find ${BASEDIR} | grep Dockerfile)
    sed -i 's/gcr\.io\/tetrate-istio-arm\/iptables@sha256:[0-9a-f]*/gcr\.io\/istio-release\/iptables@sha256:8efeb55ddf08f2f513d303b8f0ff42c9f08f355de2f4124e641d209d11a6af91/' ${BASEDIR}/pilot/docker/Dockerfile.proxyv2
    export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/getistio-build/proxy-arm
    export BASE_VERSION=1602e34d9524a2a312907aab276bcd7100da52df # 1.12
    
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

if [[ "$(uname -m)" = "aarch64" ]]; then
    sed -i 's/linux_amd64/linux_arm64/' pkg/model/model.go
fi

echo "Copying istio directory"
cp -r ../istio .
# export IMAGE_VERSION=$(curl https://raw.githubusercontent.com/istio/test-infra/master/prow/config/jobs/release-builder.yaml | grep "image: gcr.io" | head -n 1 | cut -d: -f3)
# make shell TODO: https://github.com/tetratelabs/getistio/issues/82

# "Enabling CGO for FIPS build via CGO_ENABLED=1"
echo "Enabling CGO for FIPS build via CGO_ENABLED=1 to istio/common/scripts/gobuild.sh"

if [[ ${TAG} =~ "fips" ]]; then
  export CGO_ENABLED=1
fi

# Generalizing TAG variable exporting option to incorporate ARM build.We need amd64 and arm64 suffix in docker images to create multi-arch images.Not needed for tetrate and tetratefips build.
if [[ ${TAG} =~ "multiarch" ]]; then
  if  [[ "$(uname -m)" = "aarch64" ]]; then
    export TAG="${TAG}-arm64"
  else
    export TAG="${TAG}-amd64"
  fi
fi

#install rpm-build package
sudo apt-get install rpm -y
# Build Docker Images
sudo rm -rf /tmp/istio-release && mkdir /tmp/istio-release

if [[ ${TAG} =~ "fips" ]]; then
  GOEXPERIMENT=boringcrypto go run main.go build --manifest manifest.docker.yaml
else
  go run main.go build --manifest manifest.docker.yaml
fi
# go run main.go validate --release /tmp/istio-release/out # seems like it fails if not all the targets are generated

#loading pilot image manually since docker container create command is failing due to unavailbilty of pilot image locally
docker load -i /tmp/istio-release/out/docker/pilot.tar.gz

CONTAINER_ID=$(docker create $HUB/pilot:$TAG)
docker cp $CONTAINER_ID:/usr/local/bin/pilot-discovery pilot-bin
# go version with which the binaries for the docker images wi
BUILD_GO_VERSION=$(go version pilot-bin | cut -f2 -d" ")
echo "Images are built with: go $BUILD_GO_VERSION"

[ $BUILD_GO_VERSION == go$GOLANG_VERSION ] || exit 1

# Check if binaries are compiled with boringcrypto
if [ ${TAG} =~ "fips" ]; then
    CHECK_CRYPTO=$(go version pilot-bin| cut -f3 -d" ") 
    [[ $CHECK_CRYPTO == X:boringcrypto ]] || exit 1
fi

go run main.go publish --release /tmp/istio-release/out --dockerhub $HUB
echo "Cleaning up the istio source artificats...."
sudo rm -rf /tmp/istio-release/sources/

if [[ "$(uname -m)" = "x86_64" ]]; then
    export TAG="${TAG%-amd64}"
    ${BASEDIR}/tetrateci/gen_release_manifest.py ${BASEDIR}/../release-builder/example/manifest.yaml ${BASEDIR}/../release-builder/
else
    exit 0
fi

# If RELEASE, Build Archives
if [[ -z ${TEST:-} ]]; then
    echo "Building archives..."
    # if FIPS, need to use native go as boringgo as of now can't build archives for different platforms
    if [[ ${TAG} =~ "fips" ]]; then
        sudo rm -rf /usr/local/go
        source ${BASEDIR}/tetrateci/setup_go.sh
        #disabling cgo flag
        export CGO_ENABLED=0
    fi
    echo "Cleaning up older artifacts created in docker build stage ..."
    sudo rm -rf /tmp/istio-release/sources/ && sudo rm -rf /tmp/istio-release/work/
    echo "Prunning docker images to reclaim more space for 1.13.x-fips release"
    for i in `docker images | grep -i app_sidecar | awk {'print $3'} | tail -n +2`; do echo pruning $i; docker rmi $i --force; done
    go run main.go build --manifest manifest.archive.yaml

    python3 -m pip install --upgrade cloudsmith-cli --user
    export PATH=$PATH:/home/runner/.local/bin

    PACKAGES=$(ls /tmp/istio-release/out/ | grep "istio")
    for package in $PACKAGES; do
        echo "Publishing $package"
        cloudsmith push raw tetrate/getistio /tmp/istio-release/out/$package
    done
fi
echo "Cleaning /tmp/istio...."
#[ -d "/tmp/istio-release" ] && sudo rm -rf /tmp/istio-release

echo "Done building and pushing the artifacts."
