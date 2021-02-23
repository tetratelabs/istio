#!/usr/bin/env bash

set -o errexit
set -o pipefail

NEWTAG=$TAG-istio-v0

# exit if the tag already exist
curl $( sed "s/content/packages/g" <<< $BINTRAY_API )| jq ".versions[]" | grep -q "$NEWTAG" && exit

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
    rm -f /tmp/curl.out
    curl -T ./$package -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$NEWTAG/$package -o /tmp/curl.out
    cat /tmp/curl.out
    grep "success" /tmp/curl.out
done

rm -f /tmp/curl.out
curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$NEWTAG/publish -o /tmp/curl.out
cat /tmp/curl.out

echo "Cleaning up the the downloaded artifacts"

cd ..
rm -rf release
