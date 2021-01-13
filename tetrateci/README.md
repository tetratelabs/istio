## Setting up the CI/CD system

###### Required Creds
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
If the workflow fails, the logs should be pretty clear why it failed. If still the problem cant't be debugged, the best suggestion would be to add the following workflow and ssh into the runner to run the steps manually. Or the copy the debug job into the workflow that needs to be debugged.

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
More info can be found [here](https://github.com/luchihoratiu/debug-via-ssh).
