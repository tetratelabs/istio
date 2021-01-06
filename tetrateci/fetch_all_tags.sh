#!/bin/bash
UPSTREAM_REPO="https://${GITHUB_ACTOR}:${GITHUB_TOKEN}@github.com/istio/istio.git"
git remote add upstream $UPSTREAM_REPO
git fetch --tags upstream
git push --tags origin