## Scripts for TSB test automation
There are 3 scripts which are to be used for generating yamls for installing apps with different configurations in TSB. All of the follow a more or less homogeneous naming scheme, which looks like
 
 - Tenant : `tenant<tenant_id>`
 - Workspace: `<app>t<tenant_id>ws<workspace_id>`
 - Groups: `<app>t<tenant_id>w<workspace_id><mode><group_type><group_id>`
 - Namespaces: `t<tenant_id>w<workspace_id><cluster_name><app>n<mode><namespace_id><namespace_type>`

 Here ID is just a non negative unique number to differentiate between entities and is based on the order. App is the application shorthand which is to be installed, for example for Bookinfo it is `bkif` for HttpBin it is `htbn`. Mode is either `BRIDGED` or `DIRECT` whichever we chose. Group Type is the shorthand for the same, Gateway Group becomes `gg`, Traffic Group `tg` and Security Group `sg`. Namespace Type is only useful for the MultiGateway Bookinfo installation, other than that, it is `f` for all the cases representing front which takes up the incoming request from the user.

 ```bash
 <folder>
    |-> certs/
    |-> k8s_objects/
    |-> tsb_objects/
        |-> bridged/
    |-> tsb_k8s_objects/
    |-> config.yaml
 ```

All the scripts take an optional argument `--folder` to mention the folder we want the generated templates to be in. If not mentioned, the folder name would be the current UNIX timestamp. The folder structure varies a little script by script but overall it looks like the above one. The `certs` contains all the certificates generated with openssl, `k8s_objects` contains all the files to be applied through `kubectl` and `tsb_objects` contains the files to be applied through `tctl`, the `tsb_k8s_objects` is only applicable for bookinfo instances for now. The `config.yaml` is the config used to generate the yamls, kept their just for reference.

> **Note:** `openssl` binary needs to be installed and in path to generate the scripts.

#### Multigateway Bookinfo
`bkif_multi.py` script is the one responsible to generate this config. It takes 3 arguments, 2 optional and 1 mandatory. Example:
```bash
pipenv run python bkif_multi.py --config config.example.yml --password E*oWGjD4Zf61IZ%i --folder apple
```
`config` is mandatory, we pass in the configuration file for generating the yaml with this. One of the example configuration look like this, 
```yaml
org: "tetrate"
provider: "others" # the management plane provider, possible values "aws", "others"
tctl_version: "1.2.0" # tctl version to be used to edit serviceroute
# an array of configuration
app:
- replicas:
  - bridged: 1
    direct: 0
    tenant_id: 0
  - bridged: 0
    direct: 1
    tenant_id: 1
  cluster_name: "demo"
  traffic_gen_ip: "internal" # possible values: `external` or `internal`
```
We pass in the name of the organization with `org`, the kubernetes cluster provider with `provider` with whether it is `aws` or anything else, this is necessary cause `aws` uses hostname instead of an IP and the `tctl_version` is just make sure we use a compatible `tctl` to simulate editing of the serviceroute files in the cluster. 

The `app` is an array of configuration which differ by the `cluster_name`, there couldn't be 2 entries with the same `cluster_name` while the rest could stay same. The `traffic_gen_ip` could be either `external` or `internal` based on which one of the IP we want to use to send traffic to our cluster.

The `replicas` array is the one which track the number of instances of bookinfo which is to be installed. Number of `bridged` and `direct` instances are mentioned with the corresponding entry. The `tenant_id` is the tenant under which they all belong. The `tenant_id` is independent of the rest and the same `tenant_id` could be used for other `replica` or even other `app`. The script will generate all the tenants by unique `tenant_ids`.

Coming back to the flags, the `password` is the password of the admin user of the tsb cluster we are going to install our app, this is for editing serviceroute files.

After generating the yamls, there would be the tenant yamls in the root directory, which needs to be applied first before the TSB objects. Under the `k8s_objects`, `tsb_objects` and `tsb_k8s_objects`, the yamls would be separated by another level of folders with `<cluster_name>-<order>` so that we could differentiate between different bookinfo instances.

Every bookinfo instance would have its own workspace, gateway group, traffic group and security group. Added to that they would have 3 namespaces, with namespace types `f`, `m` and `b`, corresponding to front, mid and back. The front would contain the productpage service, mid would contain the ratings service and the back would contain, reviews and details service.

#### Single Gateway HttpBin
`htbn_single.py` script is the one responsible to generate this config. It takes 2 arguments, 1 optional and 1 mandatory. Example:
```bash
pipenv run python htbn_single.py --config httpbin-config.example.yaml --folder orange
```
Same as above `config` is mandatory, though the configuration is far simpler than bookinfo, here is the one which comes with it,
```yaml
count: 4
org: "tetrate"
cluster: "demo"
mode: "bridged" # can be "direct" or "bridged"
```
Number of instances of helloworld we want, the organisation and the cluster name. In addition to that, we get an option to chose modes in the config.

The difference between this script and the previous one is, this just installs a single service called httpbin multiple times in a single namespace with different names. It also uses a single gateway and the traffic is routed based on the hostname provided. For SSL it uses a wildcard certificate just to make things a bit simpler to understand. And unlike previous one, it dumps everything on `tsb_objects` and `k8s_objects` no need to distinguish between different installs.

#### Single Gateway Bookinfo
`bkif_single.py` script is the one responsible to generate this config. It takes 2 arguments, 1 optional and 1 mandatory. Example:
```bash
pipenv run python bkif_single.py --config bookinfo-single.example.yml --folder mango
```
It is exactly the same thing as httpbin one but for bookinfo.
```yaml
count: 3
org: "tetrate"
cluster: "demo"
mode: "bridged" # can be "direct" or "bridged"
```

It also installs all the entities in a single namespace, under one set of workspace, groups and tenant. 


#### Hacking

The code is organized into 8 python files, out of which 3 are the one which is to be used directly and rest 5 just have the common functions which are being shared among all 3 of them. The `certs.py` has the functions to generate certificates and the corresponding the k8s secret. It requires openssl to be installed on the machine. `config.py` contains the functions for reading the configuration for the bookinfo single gateway script, since the script is a bit long it was decided to separate out the config part. 

The `common.py`, `tsb_objects.py` and `k8s_objects.py` go together with the first one containing the functions to render templates and other 2 just some wrappers for that. `jinja2` is being used to render the templates, for most cases we just unpack a dictionary as named arguments into the render function to reduce code repetation. All the templates are store in the `templates` folder. The single gateway scripts were an after thought, so there could be slight inconsistencies.

If changes are being introduced in the codebase, any faults can quickly identified using the `changes_check.py` it will verify the changes against a previously generated config which works correctly and report if there are any discrepencies.
