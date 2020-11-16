# How to sync with upstream Istio

1. Sync the upstream-istio branch with istio

```shell
# set this variable to the name of a branch from `istio/istio` you want to merge in
UPSTREAM_ISTIO_BRANCH=istio/release-1.7

git clone git@github.com:tetrateio/istio.git
cd istio/
git checkout -b upstream-istio

# compute diff between the HEAD and the last merged in commit from `istio/istio`.
# essentially, these are the changes that we need to apply on top of the latest state of `istio/istio`.
# some of the changes might have already been upstreamed.
# in practice, the diff is not that big. E.g., the diff from Istio 1.6.8 was the following:
#
# ```
# $ git diff --name-only $( grep '^commit ' UPSTREAM-SHA | awk '{print $2}' ) HEAD
#
# .circleci/config.yml
# README_CUSTOM_ENVOY_BINARY.md
# README_UPDATING_ISTIO.md
# UPSTREAM-SHA
# install/vm/Chart.yaml
# install/vm/charts/sidecar/Chart.yaml
# install/vm/charts/sidecar/templates/docker-compose.yaml
# install/vm/charts/sidecar/values.yaml
# install/vm/values.yaml
# operator/pkg/vfs/assets.gen.go
# pilot/pkg/model/service.go
# pilot/pkg/security/model/authentication.go
# pilot/pkg/serviceregistry/external/conversion.go
# pilot/pkg/serviceregistry/external/conversion_test.go
# pilot/pkg/serviceregistry/kube/controller/controller_test.go
# pilot/pkg/serviceregistry/kube/conversion.go
# pilot/pkg/serviceregistry/kube/conversion_test.go
# ```
git diff $( grep '^commit ' UPSTREAM-SHA | awk '{print $2}' ) HEAD >fork.diff

git remote add istio https://github.com/istio/istio
git fetch istio

git merge --strategy=recursive --strategy-option theirs ${UPSTREAM_ISTIO_BRANCH}

# manually merge remaining conflicts (by accepting their changes everywhere)

# ... your actions ...

git merge --continue

# apply the diff we created in the beginning
git apply --ignore-space-change --reject fork.diff

# manually resolve rejected chunks (e.g., because they have already been upstreamed)

# ... your actions ...

git commit -m 'apply fork diff'

# update UPSTREAM-SHA with info about the commit from `istio/istio` you've just merged in
git log -1 ${UPSTREAM_ISTIO_BRANCH} >UPSTREAM-SHA
git add UPSTREAM-SHA
git commit -m 'update UPSTREAM-SHA'

# compute the new diff from upstream istio and verify visually that is makes sense
git diff $( grep '^commit ' UPSTREAM-SHA | awk '{print $2}' ) HEAD >new.diff

# E.g., after upgrading from Istio 1.6.8 to 1.7.2 the new diff looked the following way
# ```
# $ git diff --name-only $( grep '^commit ' UPSTREAM-SHA | awk '{print $2}' ) HEAD
#
# .circleci/config.yml
# README_CUSTOM_ENVOY_BINARY.md
# README_UPDATING_ISTIO.md
# UPSTREAM-SHA
# install/vm/Chart.yaml
# install/vm/charts/sidecar/Chart.yaml
# install/vm/charts/sidecar/templates/docker-compose.yaml
# install/vm/charts/sidecar/values.yaml
# install/vm/values.yaml
# operator/pkg/vfs/assets.gen.go
# pilot/pkg/security/model/authentication.go
# ```
# Notice that it became smaller than before since some of the changes have been upstreamed.

# push the changes and open a PR against the default branch (`tcc` rather than `master`)
git push --set-upstream origin upstream-istio
```

1. On commit, a new build will automatially be created in Cicrcle CI, however, paused at a manual approval step.

Go to [CircleCI](https://app.circleci.com/pipelines/github/tetrateio/istio) and approve the build to get all Istio containers
rebuilt.

This step will reveal any merge conflict leftovers.

1. Alternatively, you can rebuild all Istio docker containers by running the `trigger_istio_release.sh` script below.

NOTE(yskopets): the following instructions didn't work for me as of September 2020.
                Maybe there have been some change on CircleCI side, but the job launched that way
                picks its configuration (normally, `.circleci/config.yml`) from somewhere else
                and fails because of that.

```bash
#!/bin/bash
set +x
BRANCH=istio-update
SHORTSHA=`git rev-parse --short HEAD`
DATE=`date +"%Y-%m-%d-%H-%M"`
TSB_VERSION=0.8.4
ISTIO_VERSION=1.7.2
TAG=${TSB_VERSION}-istio-${ISTIO_VERSION}-${SHORTSHA}
# istio version

echo $TAG

# MAKE SURE TO SET CIRCLE_CI_PERSONAL_API_TOKEN with the token from circleci
curl -X POST --header "Content-Type: application/json" -d '{
  "build_parameters": {
    "CIRCLE_JOB" : "dockerpush",
    "HUB" : "docker.io/tetrate",
    "TAG" : '\"${TSB_VERSION}\"',
    "VERSION" : '\"${ISTIO_VERSION}\"'
  }
}
' "https://circleci.com/api/v1.1/project/github/tetrateio/istio/tree/${BRANCH}?circle-token=${CIRCLE_CI_PERSONAL_API_TOKEN}"
```

Wait for ~20 minutes for the images to build.

1. Get PR approved and merged into the default branch (`tcc` rather than `master`)

1. Update [dependencies.sh](https://github.com/tetrateio/tetrate/blob/master/dependencies.sh) in [tetrateio/tetrate](https://github.com/tetrateio/tetrate) with the `TAG` and the commit `SHA`.

1. It is encouraged to update the `istio.io/istio`'s replace entry in [go.mod](https://github.com/tetrateio/tetrate/blob/master/go.mod) to use the latest SHA of this repo (`tcc` branch).

```shell
$ go mod edit -replace istio.io/istio=github.com/tetrateio/istio@<LATEST_SHA_OF_THIS_REPO>
# For example
$ go mod edit -replace istio.io/istio=github.com/tetrateio/istio@23393db
```
