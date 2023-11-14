## Github Workflows
### backport_commits.yml
Creates a backports PR for any commit made to the `tetrate-workflow` branch to any branch which conform those regexes
- `origin/tetrate-release-[[:digit:]]+.[[:digit:]]+$`
- `origin/tetratefips-release-[[:digit:]]+.[[:digit:]]+$` 

The script is adapted from [here](https://github.com/repo-sync/pull-request). The catch is the script only ports changes if there are on the `tetrateci` or `.github/workflows` folders. Merging with a commit or rebasing is not used to avoid merge conflicts.

### sync_fork.yml
Runs every midnight and checks if there are any new tag on `istio/istio`, if no new tags are founds the action is done. On the other hand if there are new tags, the script loops through them creating corresponding `test-tetrate-x.y.z-v0` & `test-tetratefips-x.y.z-v0` tags and merging them with `tetrate-release-x.y` & `tetratefips-release-x.y` branches.

Also pushes the archives from `istio/istio` release to the cloudsmith repo.

### e2e_tests.yml
Runs if there are any tags pushed with `test-` prefix. Utilizes `istio/release-builder` to generate docker images which are to be used for subsequent testing. The one thing to keep in mind it, the same script is used for making the releases and we only differentiate that based on whether the `TEST` environment variable is defined or not.

Subsequent 4 jobs runs the istio integration tests on applicable versions of eks, gke, aks and eksd, though some of them are disable for various reasons for now. All the platforms have corresponding create and cleanup scripts. The `version_check.py` has a matrix of istio versions vs k8s versions which determines which versions we need to get the istio release tested on.

All the minor versions of istio have their own testing scripts, the reason being there are patches which need to be applied before testing so the tests dont fail. The tests being written for `kind` have some default assumptions which might not be applicable for all the platforms we test on.

After the tests pass, the `test-` prefix is stripped off the current tag and the tree is tagged with the remaining, cutting a release with something similar to `tetrate-x.y.z-vn`.

Then the release builder is again triggered to create the release images and archives with `tetrate-x.y.z-vn` tag but this time without defining the `TEST` environment variable. A fips compliant build is only triggered if the tag contains `fips` in it. The only difference between fips and non fips build is the `Go` we are using. The `create_istio_release.sh` script sets up the environment manually instead if using the docker image is due to some restrictions in the Github Actions, it becomes a bit hard to procure the logs and monitor the whole process.

### make_release.yml
It is same as the last process of `e2e_tests.yml` but with a manual trigger. Changes made to any of them must be backported to the other one, since they more or less do the same thing.
