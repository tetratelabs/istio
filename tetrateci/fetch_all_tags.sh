#!/usr/bin/env bash
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
    if [[ ! $(git rev-parse --verify --quiet origin/tetrate-release-$branch) ]]; then
        # create the tetrate release branch if it doesn't exist with the workflows
        git checkout -b tetrate-release-$branch origin/tetrate-workflow
        git merge $tag --no-edit --allow-unrelated-histories -X theirs
        git tag test-$tag-tetrate-v0
    else
        git checkout -b tetrate-release-$branch origin/tetrate-release-$branch
        git merge $tag --no-edit --allow-unrelated-histories -X theirs
        git tag test-$tag-tetrate-v0
    fi

    git push origin tetrate-release-$branch --tags

    # Now for FIPS
    if [[ ! $(git rev-parse --verify --quiet origin/tetratefips-release-$branch) ]]; then
        git checkout -b tetratefips-release-$branch origin/tetrate-workflow
        git merge $tag --no-edit --allow-unrelated-histories -X theirs
        # no tag created since we need to backport the corresponding patch for fips compliant build manually
    else
        git checkout -b tetratefips-release-$branch origin/tetratefips-release-$branch
        git merge $tag --no-edit --allow-unrelated-histories -X theirs
        git tag test-$tag-tetratefips-v0
    fi

    git push origin tetratefips-release-$branch --tags

    export TAG=$tag
    ./tetrateci/upload_upstream_release.sh
done

# finally push all the tags
git push --tags origin
