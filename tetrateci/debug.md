#### Debugging
If the workflow fails, the logs should be pretty clear why it failed. If still the problem cant't be understood, the suggestion would be to add the following and ssh into the runner to run the steps manually. Or copy the debug job into the workflow that needs to be debugged. Some default GITHUB ACTION environment variablesare not seen in the shell session - just something to be vary of while debugging.

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
