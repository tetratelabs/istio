import urllib.request
import os, sys, shutil, platform
import argparse
import config
import certs
from subprocess import PIPE, Popen
from jinja2 import Template
import yaml

# https://stackoverflow.com/a/23796709
def cmdline(command):
    process = Popen(args=command, stdout=PIPE, shell=True)
    return process.communicate()[0]

def print_cmdline(command):
    # print(str(cmdline(command), "utf-8"), end="")
    pass

def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()

cleanup_script = ""

def create_namespace(ns, labels, fname):
    yamlcontent = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"name": ns, "labels": labels},
    }

    f = open(fname, "w")
    yaml.safe_dump(yamlcontent, f)
    f.close()

def install_bookinfo(conf, default_context, tenant_index):
    global cleanup_script

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
        reviews_domain = (
            svc_domain
            if conf.reviews.cluster_hostname is None
            else conf.reviews.cluster_hostname
        )
        details_domain = (
            svc_domain
            if conf.details is None or conf.details.cluster_hostname is None
            else conf.details.cluster_hostname
        )
        details_env = "details." + detailsns + details_domain
        reviews_env = "reviews." + reviewsns + reviews_domain
        ratings_env = "ratings." + reviewsns + reviews_domain

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
        t = open(conf.workspace_yaml)
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
        t = open(conf.groups_yaml)
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
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/groups.yaml", r)

        # perm
        t = open(conf.perm_yaml)
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
        t = open(conf.security_yaml)
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

        if conf.details is not None and conf.details.context is not None:
            switch_context(conf.details.context)
        else:
            switch_context(default_context)

        create_namespace(
            detailsns,
            {"istio-injection": "enabled"},
            "genned/" + key + "/k8s-objects/detailsns.yaml",
        )

        i += 1

        if conf.reviews.context is not None:
            switch_context(conf.reviews.context)
        else:
            switch_context(default_context)

        create_namespace(
            reviewsns,
            {"istio-injection": "enabled"},
            "genned/" + key + "/k8s-objects/reviewsns.yaml",
        )

        # gateway
        t = open(conf.reviews.virtualservice_yaml)
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
            workspaceName=workspace_name,
            groupName=traffic_group,
            hostFQDN="reviews." + reviewsns + ".svc.cluster.local"
            if conf.reviews.cluster_hostname is None
            else conf.reviews.cluster_hostname,
            serviceRouteName="bookinfo-serviceroute",  # need to change
        )
        save_file("genned/" + key + "/tsb-objects/serviceroute.yaml", r)
        t.close()

        i += 1

        if conf.product.context is not None:
            switch_context(conf.product.context)
        else:
            switch_context(default_context)

        create_namespace(
            productns,
            {"istio-injection": "enabled"},
            "genned/" + key + "/k8s-objects/productns.yaml",
        )

        certs.create_private_key(productns)
        certs.create_cert(productns)
        certs.create_secret(productns, "genned/" + key + "/k8s-objects/secret.yaml")

        # gateway
        t = open(conf.product.gateway_yaml)
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
            hostFQDN="productpage." + productns + ".svc.cluster.local"
            if conf.product.cluster_hostname is None
            else conf.product.cluster_hostname,
        )
        t.close()
        save_file("genned/" + key + "/tsb-objects/gateway.yaml", r)

        # ingress
        t = open("./k8s-objects/ingress.yaml")
        template = Template(t.read())
        r = template.render(
            ns=productns,
        )
        t.close()
        save_file("genned/" + key + "/k8s-objects/ingress.yaml", r)

        # trafficgen
        t = open("./k8s-objects/role.yaml")
        template = Template(t.read())
        r = template.render(targetNS=productns, clientNS=productns)
        t.close()
        save_file("genned/" + key + "/k8s-objects/role.yaml", r)

        # trafficgen
        t = open("./k8s-objects/traffic-gen.yaml")
        template = Template(t.read())
        r = template.render(ns=productns, hostname=productns + ".k8s.local")
        t.close()
        save_file("genned/" + key + "/k8s-objects/traffic-gen.yaml", r)

        i += 1

        print("Bookinfo installed\n")

def switch_context(context):
    global cleanup_script
    cmd = "kubectl config use-context " + context
    print("Switching Context | Running: " + cmd)
    print_cmdline(cmd)
    cleanup_script += cmd + "\n"

def main():
    global cleanup_script

    parser = argparse.ArgumentParser(description="Spin up bookinfo instances")

    parser.add_argument("--config", help="the istio version tag to be installed")
    args = parser.parse_args()

    if args.config is None:
        print("Pass in the config file with the `--config` flag")
        sys.exit(1)

    configs = config.read_config_yaml(args.config)

    default_context = str(cmdline("kubectl config current-context"), "utf-8")

    certs.create_root_cert()

    index = 0

    for conf in configs:
        if conf.context is not None:
            switch_context(conf.context)
        else:
            switch_context(default_context)

        tenant_name = "bookinfo-tenant-" + str(index)
        t = open(conf.tenant_yaml)
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
        )
        t.close()
        os.mkdir("genned")
        save_file("genned/tenant.yaml", r)
        install_bookinfo(conf, default_context, str(index))
    index += 1

    f = open("./cleanup.sh", "w")
    # f.write(cleanup_script)
    f.close()

    print("Run `bash cleanup.sh` for cleaning up all the resources including istio.")

if __name__ == "__main__":
    main()
