# Release Process

##  Make_release workflow.

This workflow will create docker images for various istio components, istioctl binaries for all the OS distros and push them to cloud-smith.This workflow creates two types of builds based on the tag.

1. tetratefips release
- If the tag has fips keyword in the tag (x.xx.x-tetratefips-vx), it will create a tetrate fips build which will be compiled using boringgo with fips build of envoy-proxy.

2. tetrate release
- If the tag does not contain fips keyword, it will be build using native go with upstream envoy proxy.


## Create a Release using make_release workflow.

This workflow needs to be run after e2e workflow i.e after created test docker image and running them through integration test-suite on aws and eks environment.Once the e2e test result is fine, this workflow can create the images and artifacts for istio build and push them to cloud-smith repository.

1. Create a tag in x.xx.x-tetrate-v0 or x.xx.x-tetratefips-v0  on respective release branch     depending on the requirement, the later will generate fips build of istio.

2. This workflows has 2 Jobs, first one creats a fips compliant proxy depending on the tag eg(x.xx.x-tetratefips-vx), if not this job is skipped and the second job release-builder-run will create the build.

3. (Optional) login to cloud-smith and check if the binaries and docker images are available.
 docker images are store in tetrate/getistio-containers repo and artifacts are saved in etrate/getistio repo.

 ## Publish the build to TID website https://istio.tetratelabs.io/

 1. clone  getmesh repo https://github.com/tetratelabs/getmesh.git
 2. Update site/manifest.json with the new release attributes like release version, eol , flavor etc.

 {
  "istio_minor_versions_eol_dates": {
    "1.13": "2023-02-11",
    "1.12": "2022-11-18",
    "1.11": "2022-10-11",
    "1.10": "2022-07-17"
  },
  "istio_distributions": [
    {
      "version": "1.13.2",
      "flavor": "tetrate",
      "flavor_version": 0,
      "k8s_versions": [
        "1.20",
        "1.21",
        "1.22",
        "1.23"
      ],
      "release_notes": [
        "https://istio.io/latest/news/releases/1.13.x/announcing-1.13.2/"
      ],
      "is_security_patch": false
    },
    {
      "version": "1.13.2",
      "flavor": "tetratefips",
      "flavor_version": 0,
      "k8s_versions": [
        "1.20",
        "1.21",
        "1.22",
        "1.23"
      ],
      "release_notes": [
        "https://istio.io/latest/news/releases/1.13.x/announcing-1.13.2/"
      ],
      "is_security_patch": false
    },


 3. Push the changes to remote branch, which will trigger CI which takes care of  docs build and run unit and e2e tests for new release and push to the website.

