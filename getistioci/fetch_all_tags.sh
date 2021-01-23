#!/bin/bash
set -o errexit
set -o pipefail
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
echo "==================="
echo "New istio tags to be created $tags"
echo "==================="

git config user.name github-actions
git config user.email github-actions@github.com

for tag in $tags; do
    # the branch names are suffixed with the first 2 numbers in the version
    branch=$( echo $tag | cut -d. -f1,2 )
    if [[ ! $(git rev-parse --verify --quiet origin/getistio-release-$branch) ]]; then
        # create the getistio release branch if it doesn't exist with the workflows
        git checkout -b getistio-release-$branch origin/tetrate-workflow
    else
        git checkout -b getistio-release-$branch origin/getistio-release-$branch
    fi
    git merge $tag --no-edit --allow-unrelated-histories
    git tag test-$tag-getistio-v0
    git push origin getistio-release-$branch --tags
done

# finally push all the tags
git push --tags origin
