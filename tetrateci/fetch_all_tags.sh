#!/bin/bash

# we first fetch all the tags from origin and then from upstream
# the tag should be in form of number.number.number
git remote add upstream https://github.com/istio/istio
git fetch --tags origin
git tag -l | grep -E "^[0-9]+.[0-9]+.[0-9]+$" > /tmp/oldtags
git fetch --tags upstream
git tag -l | grep -E "^[0-9]+.[0-9]+.[0-9]+$" > /tmp/newtags

echo "print oldtags"
cat /tmp/oldtags
echo "print newtags"
cat /tmp/newtags

# then compare the list and pick the ones unique to the second list
tags=$(comm -13 /tmp/oldtags /tmp/newtags)

git config user.name github-actions
git config user.email github-actions@github.com

for tag in $tags; do
    # the branch names are suffixed with the first 2 numbers in the version
    branch=$( echo $tag | cut -d. -f1,2 )
    if [[ ! $(git rev-parse --verify --quiet origin/tetrate-release-$branch) ]]; then
        # create the tetrate release branch if it doesn't exist with the workflows
        git checkout tetrate-workflow
        git checkout -b tetrate-release-$branch 
    else
        git checkout -b tetrate-release-$branch origin/tetrate-release-$branch
    fi
    git merge $tag --no-edit
    git tag tetrate-test-$tag
    git push origin tetrate-release-$branch --tags
done

# finally push all the tags
git push --tags origin
