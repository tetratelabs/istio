import urllib.request
import os, sys, shutil, platform
import argparse
import config
import certs
from subprocess import PIPE, Popen
from jinja2 import Template
import yaml

def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()

def create_namespace(ns, labels, fname):
    yamlcontent = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"name": ns, "labels": labels},
    }

    f = open(fname, "w")
    yaml.safe_dump(yamlcontent, f)
    f.close()

def install_bookinfo(conf, tenant_index):
    tenant_name = "bookinfo-tenant-" + tenant_index

    i = 0

    while i < conf.replicas * 3:
        print("Installing Bookinfo")
        key = str(int(i / 3))
        workspace_name = "bookinfo-ws-" + key
        os.mkdir("genned/" + key)
        os.mkdir("genned/" + key + "/k8s-objects")
        os.mkdir("genned/" + key + "/tsb-objects")

        productns = "bookinfo-b" + key + "-t" + tenant_index + "-front"
        detailsns = "bookinfo-b" + key + "-t" + tenant_index + "-mid"
        reviewsns = "bookinfo-b" + key + "-t" + tenant_index + "-back"

        svc_domain = ".svc.cluster.local"
        details_env = "details." + detailsns + svc_domain
        reviews_env = "reviews." + reviewsns + svc_domain
        ratings_env = "ratings." + reviewsns + svc_domain

        t = open("k8s-objects/bookinfo.yaml")
        template = Template(t.read())
        r = template.render(
            reviewsns=reviewsns,
            detailsns=detailsns,
            productns=productns,
            detailsHostName=details_env,
            reviewsHostName=reviews_env,
            ratingsHostName=ratings_env,
        )
        t.close()
        save_file("genned/" + key + "/k8s-objects/bookinfo.yaml", r)

        # workspace
        t = open("tsb-objects/workspace.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            ns1=reviewsns,
            ns2=detailsns,
            ns3=productns,
            clusterName=conf.cluster_name,
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/workspaces.yaml", r)

        # groups
        gateway_group = "bookinfo-gateway-" + key
        traffic_group = "bookinfo-traffic-" + key
        security_group = "bookinfo-security-" + key
        t = open("tsb-objects/group.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            gatewayGroupName=gateway_group,
            trafficGroupName=traffic_group,
            securityGroupName=security_group,
            productNs=productns,
            reviewsNs=reviewsns,
            detailsNs=detailsns,
            clusterName=conf.cluster_name,
            mode=conf.mode.upper(),
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/groups.yaml", r)

        # perm
        t = open("tsb-objects/perm.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            trafficGroupName=traffic_group,
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/perm.yaml", r)

        # security
        t = open("tsb-objects/security.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            securitySettingName="bookinfo-security-setting",  # need to change
            securityGroupName=security_group,
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/security.yaml", r)

        create_namespace(
            detailsns,
            {"istio-injection": "enabled"},
            "genned/" + key + "/k8s-objects/detailsns.yaml",
        )

        i += 1

        create_namespace(
            reviewsns,
            {"istio-injection": "enabled"},
            "genned/" + key + "/k8s-objects/reviewsns.yaml",
        )

        # gateway
        t = open("tsb-objects/serviceroute.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            groupName=traffic_group,
            hostFQDN="reviews." + reviewsns + ".svc.cluster.local",
            serviceRouteName="bookinfo-serviceroute",  # need to change
        )
        save_file("genned/" + key + "/tsb-objects/serviceroute.yaml", r)
        t.close()

        i += 1

        # gateway
        t = open("tsb-objects/gateway.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            gatewayName=productns + "-gateway",
            hostname=productns + ".k8s.local",
            caSecretName=productns + "-credential",
            gatewayGroupName=gateway_group,
            ns=productns,
            hostFQDN="productpage." + productns + ".svc.cluster.local",
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/gateway.yaml", r)

        gen_k8s_objects(productns, key)

        i += 1

        print("Bookinfo installed\n")

def gen_k8s_objects(productns, key):
    create_namespace(
        productns,
        {"istio-injection": "enabled"},
        "genned/" + key + "/k8s-objects/productns.yaml",
    )

    certs.create_private_key(productns)
    certs.create_cert(productns)
    certs.create_secret(productns, "genned/" + key + "/k8s-objects/secret.yaml")

    # ingress
    t = open("./k8s-objects/ingress.yaml")
    template = Template(t.read())
    r = template.render(
        ns=productns,
    )
    t.close()
    save_file("genned/" + key + "/k8s-objects/ingress.yaml", r)

    # trafficgen
    t = open("k8s-objects/role.yaml")
    template = Template(t.read())
    r = template.render(targetNS=productns, clientNS=productns)
    t.close()
    save_file("genned/" + key + "/k8s-objects/role.yaml", r)

    # trafficgen
    t = open("k8s-objects/traffic-gen.yaml")
    template = Template(t.read())
    r = template.render(ns=productns, hostname=productns + ".k8s.local")
    t.close()
    save_file("genned/" + key + "/k8s-objects/traffic-gen.yaml", r)

def main():
    parser = argparse.ArgumentParser(description="Spin up bookinfo instances")

    parser.add_argument("--config", help="the istio version tag to be installed")
    args = parser.parse_args()

    if args.config is None:
        print("Pass in the config file with the `--config` flag")
        sys.exit(1)

    configs = config.read_config_yaml(args.config)

    certs.create_root_cert()

    index = 0

    for conf in configs:
        tenant_name = "bookinfo-tenant-" + str(index)
        t = open("tsb-objects/tenant.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
        )
        t.close()
        os.mkdir("genned")
        save_file("genned/tenant.yaml", r)
        install_bookinfo(conf, str(index))
    index += 1

if __name__ == "__main__":
    main()
