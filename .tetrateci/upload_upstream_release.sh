#!/usr/bin/env bash

set -o errexit
set -o pipefail

NEWTAG=$TAG-istio-v0

python3 -m pip install --upgrade cloudsmith-cli --user
# exit if the tag already exist
cloudsmith ls pkgs tetrate/getistio -F json | jq -r '.data[].filename' | cut -f1-3 -d. | rev | cut -f3- -d- | rev | grep istioctl | cut -f2 -d- | uniq | grep -q "$NEWTAG" && exit

echo "Creating a temporary directory to download $TAG release assets"
mkdir /tmp/release
cd /tmp/release

echo "Fetching the download urls for the $TAG release"
urls=$(curl -H "Accept: application/vnd.github.v3+json" https://api.github.com/repos/istio/istio/releases/tags/$TAG | jq -r '.assets[] | .browser_download_url')

for url in $urls; do
    echo "Downloading from $url"
    wget $url
done

echo "Renaming packages"

istiopkgs=$(ls | grep "istio-$TAG")

for pkg in $istiopkgs; do
    name=$(sed "s/istio-$TAG/istio-$TAG-istio-v0/g" <<< $pkg)
    echo "Renaming $pkg to $name"
    mv $pkg $name
done

istioctlpkgs=$(ls | grep "istioctl-$TAG")

for pkg in $istioctlpkgs; do
    name=$(sed "s/istioctl-$TAG/istioctl-$TAG-istio-v0/g" <<< $pkg)
    echo "Renaming $pkg to $name"
    mv $pkg $name
done

PACKAGES=$(ls | grep "istio")

for package in $PACKAGES; do
    echo "Publishing $package"
    cloudsmith push raw tetrate/getistio ./$package
done

echo "Cleaning up the the downloaded artifacts"

cd ..
rm -rf release
