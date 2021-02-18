#!/bin/bash
set -o errexit
set -o pipefail

if [[ -z "$GITHUB_TOKEN" ]]; then
  echo "Undefined GITHUB_TOKEN environment variable."
  exit 1
fi

echo "Configuring git"

cat <<- EOF > $HOME/.netrc
    machine github.com
    login $GITHUB_ACTOR
    password $GITHUB_TOKEN
    machine api.github.com
    login $GITHUB_ACTOR
    password $GITHUB_TOKEN
EOF
chmod 600 $HOME/.netrc

git config user.name $GITHUB_ACTOR
git config user.email github-actions@github.com

echo "Fetching target branches"

TARGETS=$(git branch -r| grep origin/tetrate-release | xargs)

echo "Creating PRs"

for branch in $TARGETS; do
    echo "Getting branch name for $branch"
    branch_name=$(cut -f2 -d"/" <<< $branch)
    echo "Creating PR for $branch_name"
    hub pull-request -b $branch_name -m "AUTO: Backporting patches to $branch_name"
done

echo "Creating PRs for FIPS branches"

FIPS_TARGETS=$(git branch -r| grep origin/tetratefips-release | xargs)

for branch in $FIPS_TARGETS; do
    echo "Getting branch name for $branch"
    branch_name=$(cut -f2 -d"/" <<< $branch)
    echo "Creating PR for $branch_name"
    hub pull-request -b $branch_name -m "AUTO: Backporting patches to $branch_name"
done