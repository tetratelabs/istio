#!/bin/bash

set -o errexit
set -o pipefail

echo "Creating a temporary directory to download $TAG release assets"
mkdir release-tmp
cd release-tmp

echo "Fetching the download urls for the $TAG release"
urls=$(curl -H "Accept: application/vnd.github.v3+json" https://api.github.com/repos/istio/istio/releases/tags/$TAG | jq -r '.assets[] | .browser_download_url')

for url in $urls; do
    echo "Downloading from $url"
    wget $url
done

PACKAGES=$(ls | grep "istio")

for package in $PACKAGES; do
    echo "Publishing $package"
    rm -f /tmp/curl.out
    curl -T ./$package -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/$package -o /tmp/curl.out
    cat /tmp/curl.out
    grep "success" /tmp/curl.out
done

rm -f /tmp/curl.out
curl -X POST -u$BINTRAY_USER:$API_KEY $BINTRAY_API/$TAG/publish -o /tmp/curl.out
cat /tmp/curl.out

echo "Cleaning up the the downloaded artifacts"

cd ..
rm -rf release-tmp