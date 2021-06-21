import os
from jinja2 import Template
import yaml
import base64
import argparse
from dataclasses import dataclass
from marshmallow_dataclass import class_schema
import certs

@dataclass
class config:
    count: int
    org: str
    cluster: str

def read_config_yaml(filename):
    schema = class_schema(config)
    with open(filename) as file:
        yamlconfig = yaml.load(file, Loader=yaml.SafeLoader)
        return schema().load(yamlconfig)

script_path = os.path.dirname(os.path.realpath(__file__))

def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()

def install_httpbin(index, namespace):
    instance_name = "httpbin" + index
    t = open(script_path + "/templates/k8s-objects/httpbin.yaml")
    template = Template(t.read())
    r = template.render(namespace=namespace, name=instance_name)
    t.close()
    save_file("generated/k8s-objects/httpbin" + index + ".yaml", r)

def main():
    parser = argparse.ArgumentParser(
        description="Spin up httpbin instances, all the flags are required and to be pre generated\n"
        + "Example:\n"
        + " pipenv run python single_ns.py --config httpbin-config.example.yaml\n",
        formatter_class=argparse.RawTextHelpFormatter,
    )

    parser.add_argument(
        "--config", help="pass the config for the install", required=True
    )

    args = parser.parse_args()
    conf = read_config_yaml(args.config)

    tenant = "httpbin-tenant-0"
    workspace = f"httpbin-ws-{conf.cluster}-b-t0-0"
    namespace = f"httpbin-{conf.cluster}-b-t0-w0-front-0"

    namespace_yaml = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"labels": {"istio-injection": "enabled"}, "name": namespace},
    }

    os.makedirs("generated/k8s-objects/", exist_ok=True)
    os.makedirs("generated/tsb-objects/", exist_ok=True)

    t = open(script_path + "/templates/tsb-objects/tenant.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant,
    )
    t.close()
    save_file("generated/tsb-objects/tenant.yaml", r)

    t = open(script_path + "/templates/tsb-objects/workspace-httpbin.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant,
        workspaceName=workspace,
        ns=namespace,
        clusterName=conf.cluster,
    )
    t.close()
    save_file("generated/tsb-objects/workspaces.yaml", r)

    # groups = <app>-<type>-<cluster_name>-<mode>-t<tenant_id>-w<workspace_id>-<id>
    gateway_group = f"bookinfo-gateway-{conf.cluster}-b-t0-w0-0"
    traffic_group = f"bookinfo-traffic-{conf.cluster}-b-t0-w0-0"
    security_group = f"bookinfo-security-{conf.cluster}-b-t0-w0-0"
    t = open(script_path + "/templates/tsb-objects/group-httpbin.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant,
        workspaceName=workspace,
        gatewayGroupName=gateway_group,
        trafficGroupName=traffic_group,
        securityGroupName=security_group,
        ns=namespace,
        clusterName=conf.cluster,
        mode="BRIDGED",
    )
    t.close()
    save_file("generated/tsb-objects/groups.yaml", r)

    # perm
    t = open(script_path + "/templates/tsb-objects/perm.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant,
        workspaceName=workspace,
        trafficGroupName=traffic_group,
    )
    t.close()
    save_file("generated/tsb-objects/perm.yaml", r)

    t = open(script_path + "/templates/tsb-objects/bridged/security.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant,
        workspaceName=workspace,
        securitySettingName="httpbin-security-setting-" + namespace,
        securityGroupName=security_group,
    )
    t.close()
    save_file("generated/tsb-objects/security.yaml", r)

    f = open("generated/k8s-objects/01namespace.yaml", "w")
    yaml.dump(namespace_yaml, f)
    f.close()

    t = open(script_path + "/templates/k8s-objects/ingress.yaml")
    template = Template(t.read())
    r = template.render(ns=namespace)
    t.close()
    save_file("generated/k8s-objects/ingress.yaml", r)

    certs.generate_wildcard_cert()
    certs.create_wildcard_secret(namespace, "generated/k8s-objects/secret.yaml")
    certs.create_trafficgen_wildcard_secret(
        namespace, "generated/k8s-objects/trafficgen-secret.yaml"
    )

    http_routes = []
    curl_calls = []

    for i in range(conf.count):
        install_httpbin(str(i), namespace)
        name = "httpbin" + str(i)
        hostname = name + ".tetrate.test.com"
        http_routes.append(name)
        curl_calls.append(
            f"curl https://{hostname} --connect-to {hostname}:443:$IP:$PORT --cacert /etc/bookinfo/bookinfo-ca.crt &>/dev/null"
        )

    t = open(script_path + "/templates/tsb-objects/bridged/gateway-single.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant,
        workspaceName=workspace,
        gatewayGroupName=gateway_group,
        gatewayName="tsb-gateway",
        ns=namespace,
        entries=http_routes,
        secretName="wildcard-credential",
    )
    t.close()
    save_file("generated/tsb-objects/gateway.yaml", r)

    service_account = "httpbin-serviceaccount"
    t = open(script_path + "/templates/k8s-objects/role.yaml")
    template = Template(t.read())
    r = template.render(targetNS=namespace, clientNS=namespace, saName=service_account)
    t.close()
    save_file("generated/k8s-objects/role.yaml", r)

    t = open(script_path + "/templates/k8s-objects/traffic-gen-httpbin.yaml")
    template = Template(t.read())
    r = template.render(
        ns=namespace,
        saName=service_account,
        secretName=namespace + "-ca-cert",
        serviceName="tsb-gateway-" + namespace,
        content=curl_calls,
    )
    t.close()
    save_file("generated/k8s-objects/traffic-gen.yaml", r)

if __name__ == "__main__":
    main()
