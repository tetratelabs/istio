#!/usr/bin/env bash
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

git fetch --all --verbose
TARGETS=$(git branch -r| grep -E "origin/tetrate-release-[[:digit:]]+.[[:digit:]]+$" | xargs)

function create_pr_using_temp() {
    echo "Getting branch name for $1"
    local branch_name=$(cut -f2 -d"/" <<< $1)

    echo "Creating a temporary branch"
    git checkout -b temp-$branch_name $1

    echo "Checking out the changes"
    git checkout origin/tetrate-workflow -- tetrateci
    git checkout origin/tetrate-workflow -- .github/workflows
    git commit -m "Merging tetrate-workflow with $branch_name"

    echo  "Pushing temporary branch to origin"
    git push origin temp-github-actions-$branch_name --force

    echo "Creating PR for $branch_name"
    hub pull-request -b $branch_name -m "AUTO: Backporting patches to $branch_name"
}

echo "Creating PRs"

for branch in $TARGETS; do
    create_pr_using_temp $branch
done

echo "Creating PRs for FIPS branches"

FIPS_TARGETS=$(git branch -r| grep -E "origin/tetratefips-release-[[:digit:]]+.[[:digit:]]+$" | xargs)

for branch in $FIPS_TARGETS; do
    create_pr_using_temp $branch
done
