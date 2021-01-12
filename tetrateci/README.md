## Setting up the CI/CD system

### e2e-test and release
This repo is synced up with upstream istio.io/istio repo once every day. Post that 
it looks for any new releases made in upstream by veryfying newly created
tags of the form x.y.z. For every new tag thus found,
1. A new release branch, if it doesn't exist already, is created with
   the name tetrate-release-x.y.z using tetrate-workflow as the base branch.
1. Merge tetrate-release-x.y.z with x.y.z
1. Create a new tag `test-x.y.z-tetrate-v0` on the above commit and push

New tag of the form `test-x.y.z-tetrate-v{n}` would trigger the `e2e-tests` workflow, which verifies this new commit
of istio by running tests against different kubernetes distros. Istio
tests/integration suite is used for this purpose. `e2e-workflow` at 
the end, if everything is successful, runs the release job `make_release` 
that in turn pushes the release artifacts and images to bintray. Release
artifacts would have the tag `x.y.z-tetrate-v{n}` Release job 
uses Istio release-builder under the hood.

To apply security patches or other fixes, make the changes to the 
relevant branch and tag the commit of the form `test-x.y.z-tetrate-v{n+1}` 
before pushing it. This would trigger the `e2e-test` as described above.
For example if we want to add a security patch to istio 1.8.2 release,
checkout `test-1.8.2-tetrate-v0`, make the changes and after verification
create a tag `test-1.8.2-tetrate-v1` and push it. Created release 
artifacts would have the tag `1.8.2-tetrate-v1`

In general upstream istio `release-x.y` branch would map to `tetrate-release-x.y`
branch. One can add the patch commits to `tetrate-release-x.y` branch, tag it
and push it to trigger the `e2e-tests` and the release.

### Manual release
Sometimes, as in case of emergency or if the e2e-tests fails because
of some broken dependency or compatibility reasons, it would be needed
to bypass the e2e-tests and create a release directly. For that, one can
tag the needed commit in `tetrate-release-x.y` branch with `x.y.z-tetrate-v{n}`
and manually trigger the release workflow `Make a release`. This is 
similar to `make_release` job mentioned above. This workflow would
create the release artifacts and push them to bintray. For example if we want to apply an
emergency patch to tetrate-release-1.8 with a latest tag 1.8.2-tetrate-v1,
we could make the changes and tag the new commit directly with `1.8.2-tetrate-v2`
and trigger the `Make release` workflow manually. Newly created release
artifacts will have the tag `1.8.2-tetrate-v2`

### Required Creds
Workflows of this repo are dependent on the following github secrets: 
1. AWS_ACCESS_KEY_ID 
2. AWS_REGION
3. AWS_SECRET_ACCESS_KEY
4. AZURE_CREDENTIALS : [Note](https://github.com/Azure/login#configure-deployment-credentials)
5. BINTRAY_API_KEY
6. BINTRAY_USER
7. DEPLOY_HUB : [ Note : Link to the docker registry where final images would be pushed ]
8. GCP_PROJECT_ID
9. GCP_SA_KEY
10. PAT :  [ Note : Personal access token of a github account who can push to the repo ]
11. TEST_HUB : [ Note : Link to docker registry for pushing test images ] 
12. TEST_HUB_USER
13. TEST_HUB_PASS
14. BINTRAY_ARCHIVE_API : [ Note : The archive API url, example - https://api.bintray.com/content/hellozee/istio-archives ]
15. AZURE_RESOURCE : [ Note : The azure resource name corresponding to the creds ]

#### Debugging
If the workflow fails, the logs should be pretty clear why it failed. If still the problem cant't be debugged, the best suggestion would be to add the following workflow and ssh into the runner to run the steps manually.

```
name: Debugging with SSH
on: 
  workflow_dispatch:

jobs:
  debug:
    runs-on: ubuntu-latest
    steps:
      - name: Start SSH session
        uses: luchihoratiu/debug-via-ssh@main
        with:
          NGROK_AUTH_TOKEN: ${{ secrets.NGROK_AUTH_TOKEN }}
          SSH_PASS: ${{ secrets.SSH_PASS }}
```