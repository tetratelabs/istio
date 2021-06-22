import os
from jinja2 import Template
import yaml
import argparse
from dataclasses import dataclass
from marshmallow_dataclass import class_schema
import certs
import tsb_objects

@dataclass
class config:
    count: int
    org: str
    cluster: str
    mode: str

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

def install_bookinfo(ns, key):
    svc_domain = ".svc.cluster.local"
    details_env = "details-" + key + "." + ns + svc_domain
    reviews_env = "reviews-" + key + "." + ns + svc_domain
    ratings_env = "ratings-" + key + "." + ns + svc_domain

    t = open(script_path + "/templates/k8s-objects/bookinfo-single.yaml")
    template = Template(t.read())
    r = template.render(
        ns=ns,
        detailsHostName=details_env,
        reviewsHostName=reviews_env,
        ratingsHostName=ratings_env,
        id=key,
    )
    t.close()
    save_file("generated/k8s-objects/bookinfo-" + key + ".yaml", r)
    pass

def main():
    parser = argparse.ArgumentParser(
        description="Spin up bookinfo instances, all the flags are required and to be pre generated\n"
        + "Example:\n"
        + " pipenv run python bookinfo-single-gw.py --config bookinfo-single.example.yml\n",
        formatter_class=argparse.RawTextHelpFormatter,
    )

    parser.add_argument(
        "--config", help="pass the config for the install", required=True
    )

    args = parser.parse_args()
    conf = read_config_yaml(args.config)

    tenant = "tenant-0"
    workspace = f"bookinfo-t0-ws0"
    namespace = f"t0-w0-{conf.cluster}-bookinfo-b-front-n0"

    os.makedirs("generated/k8s-objects/", exist_ok=True)
    os.makedirs("generated/tsb-objects/", exist_ok=True)

    namespace_yaml = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"labels": {"istio-injection": "enabled"}, "name": namespace},
    }

    tsb_objects.gen_tenant(conf.org, tenant, "generated/tsb-objects/tenant.yaml")

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

    gateway_group = f"bookinfo-t0-w0-b-gg0"
    traffic_group = f"bookinfo-t0-w0-b-tg0"
    security_group = f"bookinfo-t0-w0-b-sg0"
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
        mode=conf.mode.upper(),
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
        securitySettingName="bookinfo-security-setting-" + namespace,
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
        install_bookinfo(namespace, str(i))
        name = "productpage-" + str(i)
        hostname = name + ".tetrate.test.com"

        curl_calls.append(
            f"curl https://{hostname}/productpage --connect-to {hostname}:443:$IP:$PORT --cacert /etc/bookinfo/bookinfo-ca.crt &>/dev/null"
        )

        servicerouteFile = (
            script_path + "/templates/tsb-objects/bridged/serviceroute.yaml"
        )
        if conf.mode == "bridged":
            http_routes.append(name)

            t = open(servicerouteFile)
            template = Template(t.read())
            r = template.render(
                orgName=conf.org,
                tenantName=tenant,
                workspaceName=workspace,
                groupName=traffic_group,
                hostFQDN="reviews-" + str(i) + "." + namespace + ".svc.cluster.local",
                serviceRouteName="bookinfo-serviceroute-" + str(i),
                ns=namespace,
            )
            save_file("generated/tsb-objects/serviceroute" + str(i) + ".yaml", r)
            t.close()
        else:
            http_routes.append(name)

            # virtual service for product page
            t = open(script_path + "/templates/tsb-objects/direct/vs.yaml")
            template = Template(t.read())
            r = template.render(
                orgName=conf.org,
                tenantName=tenant,
                workspaceName=workspace,
                gatewayGroupName=gateway_group,
                hostFQDN=hostname,
                virtualserviceName="bookinfo-virtualservice-"
                + str(i),  # need to change
                ns=namespace,
                gatewayName="tsb-gateway",
                destinationFQDN="productpage-"
                + str(i)
                + "."
                + namespace
                + ".svc.cluster.local",
            )
            save_file("generated/tsb-objects/virtualservice-" + str(i) + ".yaml", r)
            t.close()

            # destination rules
            t = open(script_path + "/templates/tsb-objects/direct/dr.yaml")
            template = Template(t.read())
            r = template.render(
                orgName=conf.org,
                tenantName=tenant,
                workspaceName=workspace,
                trafficGroupName=traffic_group,
                hostFQDN="reviews-" + str(i) + "." + namespace + ".svc.cluster.local",
                destinationruleName="bookinfo-destinationrule-" + str(i),
                ns=namespace,
            )
            save_file("generated/tsb-objects/destinationrule-" + str(i) + ".yaml", r)
            t.close()

            # reviews virtual service
            reviews_vs = script_path + "/templates/tsb-objects/direct/reviews-vs.yaml"
            t = open(reviews_vs)
            template = Template(t.read())
            r = template.render(
                orgName=conf.org,
                tenantName=tenant,
                workspaceName=workspace,
                trafficGroupName=traffic_group,
                hostFQDN="reviews-" + str(i) + "." + namespace + ".svc.cluster.local",
                serviceRouteName="bookinfo-reviews-" + str(i),
                ns=namespace,
            )
            save_file("generated/tsb-objects/reviews_vs-" + str(i) + ".yaml", r)
            t.close()

    if conf.mode == "bridged":
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

    else:
        t = open(script_path + "/templates/tsb-objects/direct/gw-single.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant,
            workspaceName=workspace,
            gatewayGroupName=gateway_group,
            gatewayName="tsb-gateway",
            ns=namespace,
            entries=http_routes,
            gwSecretName="wildcard-credential",
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

    pass

if __name__ == "__main__":
    main()
