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

def create_namespace(ns, labels, key):
    yamlcontent = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"name": ns, "labels": labels},
    }
    fname = "generated/" + key + "/k8s-objects/"+ ns +"ns.yaml"
    f = open(fname, "w")
    yaml.safe_dump(yamlcontent, f)
    f.close()

def generate_bookinfo_yaml(namespaces, key):
    svc_domain = ".svc.cluster.local"
    details_env = "details." + namespaces["details"] + svc_domain
    reviews_env = "reviews." + namespaces["reviews"] + svc_domain
    ratings_env = "ratings." + namespaces["reviews"] + svc_domain

    t = open("templates/k8s-objects/bookinfo.yaml")
    template = Template(t.read())
    r = template.render(
        reviewsns=namespaces["reviews"],
        detailsns=namespaces["details"],
        productns=namespaces["product"],
        detailsHostName=details_env,
        reviewsHostName=reviews_env,
        ratingsHostName=ratings_env,
    )
    t.close()
    save_file("generated/" + key + "/k8s-objects/bookinfo.yaml", r)

def gen_common_tsb_objects(namespaces, key, conf, tenant_name, workspace_name):
    # workspace
    t = open("templates/tsb-objects/workspace.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        ns1=namespaces["reviews"],
        ns2=namespaces["details"],
        ns3=namespaces["product"],
        clusterName=conf.cluster_name,
    )
    t.close()
    save_file("generated/" + key + "/tsb-objects/workspaces.yaml", r)

    # groups
    gateway_group = "bookinfo-gateway-" + key
    traffic_group = "bookinfo-traffic-" + key
    security_group = "bookinfo-security-" + key
    t = open("templates/tsb-objects/group.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        gatewayGroupName=gateway_group,
        trafficGroupName=traffic_group,
        securityGroupName=security_group,
        productNs=namespaces["product"],
        reviewsNs=namespaces["reviews"],
        detailsNs=namespaces["details"],
        clusterName=conf.cluster_name,
        mode=conf.mode.upper(),
    )
    t.close()
    save_file("generated/" + key + "/tsb-objects/groups.yaml", r)

    # perm
    t = open("templates/tsb-objects/perm.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        trafficGroupName=traffic_group,
    )
    t.close()
    save_file("generated/" + key + "/tsb-objects/perm.yaml", r)

    return gateway_group, traffic_group, security_group

def gen_namespace_yamls(namespaces, key):
    create_namespace(
        namespaces["details"],
        {"istio-injection": "enabled"},
        key,
    )

    create_namespace(
        namespaces["reviews"],
        {"istio-injection": "enabled"},
        key,
    )

    create_namespace(
        namespaces["product"],
        {"istio-injection": "enabled"},
        key,
    )

def gen_bridge_specific_objects(
    conf,
    tenant_name,
    workspace_name,
    traffic_group,
    gateway_group,
    security_group,
    namespaces,
    key,
):
    os.mkdir("generated/" + key + "/tsb-objects/bridged")
    # security
    t = open("templates/tsb-objects/bridged/security.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        securitySettingName="bookinfo-security-setting",  # need to change
        securityGroupName=security_group,
    )
    t.close()
    save_file("generated/" + key + "/tsb-objects/bridged/security.yaml", r)

    servicerouteFile = "templates/tsb-objects/bridged/serviceroute.yaml"
    t = open(servicerouteFile)
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        groupName=traffic_group,
        hostFQDN="reviews." + namespaces["reviews"] + ".svc.cluster.local",
        serviceRouteName="bookinfo-serviceroute",  # need to change
        ns=namespaces["reviews"],
    )
    save_file("generated/" + key + "/tsb-objects/bridged/serviceroute.yaml", r)
    t.close()

    gatewayFile = "templates/tsb-objects/bridged/gateway.yaml"
    t = open(gatewayFile)
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        gatewayName=namespaces["product"] + "-gateway",
        hostname=namespaces["product"] + ".tetrate.test.com",
        gwSecretName=namespaces["product"] + "-credential",
        gatewayGroupName=gateway_group,
        ns=namespaces["product"],
        hostFQDN="productpage." + namespaces["product"] + ".svc.cluster.local",
    )
    t.close()
    save_file("generated/" + key + "/tsb-objects/bridged/gateway.yaml", r)
    pass

def gen_direct_specific_objects(
    conf, tenant_name, workspace_name, traffic_group, gateway_group, namespaces, key
):
    os.mkdir("generated/" + key + "/tsb-objects/direct")
    # reviews virtual service
    reviews_vs = "templates/tsb-objects/direct/reviews-vs.yaml"
    t = open(reviews_vs)
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        groupName=traffic_group,
        hostFQDN="reviews." + namespaces["reviews"] + ".svc.cluster.local",
        serviceRouteName="bookinfo-serviceroute",  # need to change
        ns=namespaces["reviews"],
    )
    save_file("generated/" + key + "/tsb-objects/direct/reviews_vs.yaml", r)
    t.close()

    # destination rules
    t = open("templates/tsb-objects/direct/dr.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        trafficGroupName=traffic_group,
        hostFQDN="reviews." + namespaces["reviews"] + ".svc.cluster.local",
        destinationruleName="bookinfo-destinationrule",  # need to change
        ns=namespaces["reviews"],
    )
    save_file("generated/" + key + "/tsb-objects/direct/destinationrule.yaml", r)
    t.close()

    # virtual service for product page
    t = open("templates/tsb-objects/direct/vs.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        gatewayGroupName=gateway_group,
        hostFQDN=namespaces["product"] + ".tetrate.test.com",
        virtualserviceName="bookinfo-virtualservice",  # need to change
        ns=namespaces["product"],
        gatewayName=namespaces["product"] + "-gateway",
    )
    save_file("generated/" + key + "/tsb-objects/direct/virtualservice.yaml", r)
    t.close()

    # gateway
    gatewayFile = "templates/tsb-objects/direct/gw.yaml"
    t = open(gatewayFile)
    template = Template(t.read())
    r = template.render(
        orgName=conf.org,
        tenantName=tenant_name,
        workspaceName=workspace_name,
        gatewayName=namespaces["product"] + "-gateway",
        hostname=namespaces["product"] + ".tetrate.test.com",
        gwSecretName=namespaces["product"] + "-credential",
        gatewayGroupName=gateway_group,
        ns=namespaces["product"],
        hostFQDN="productpage." + namespaces["product"] + ".svc.cluster.local",
    )
    t.close()
    save_file("generated/" + key + "/tsb-objects/direct/gateway.yaml", r)

def install_bookinfo(conf, tenant_index):
    tenant_name = "bookinfo-tenant-" + tenant_index

    i = 0

    while i < conf.replicas:
        print("Installing Bookinfo")
        key = str(i)
        workspace_name = "bookinfo-ws-" + key
        os.mkdir("generated/" + key)
        os.mkdir("generated/" + key + "/k8s-objects")
        os.mkdir("generated/" + key + "/tsb-objects")

        # TODO: d for direct, b for bridged
        productns = "bookinfo-b" + key + "-t" + tenant_index + "-front"
        detailsns = "bookinfo-b" + key + "-t" + tenant_index + "-back"
        reviewsns = "bookinfo-b" + key + "-t" + tenant_index + "-mid"

        namespaces = {"product": productns, "details": detailsns, "reviews": reviewsns}

        generate_bookinfo_yaml(namespaces, key)

        gateway_group, traffic_group, security_group = gen_common_tsb_objects(
            namespaces, key, conf, tenant_name, workspace_name
        )

        gen_namespace_yamls(namespaces, key)

        if conf.mode == "bridged":
            gen_bridge_specific_objects(
                conf,
                tenant_name,
                workspace_name,
                traffic_group,
                gateway_group,
                security_group,
                namespaces,
                key,
            )
        else:
            gen_direct_specific_objects(
                conf,
                tenant_name,
                workspace_name,
                traffic_group,
                gateway_group,
                namespaces,
                key,
            )

        gen_k8s_objects(productns, key)

        print("Bookinfo installed\n")
        i += 1

def gen_k8s_objects(productns, key):

    certs.create_private_key(productns)
    certs.create_cert(productns)
    certs.create_secret(productns, "generated/" + key + "/k8s-objects/secret.yaml")

    # ingress
    t = open("templates//k8s-objects/ingress.yaml")
    template = Template(t.read())
    r = template.render(
        ns=productns,
    )
    t.close()
    save_file("generated/" + key + "/k8s-objects/ingress.yaml", r)

    service_account = productns + "-trafficegen-sa"

    # trafficgen
    t = open("templates/k8s-objects/role.yaml")
    template = Template(t.read())
    r = template.render(targetNS=productns, clientNS=productns, saName=service_account)
    t.close()
    save_file("generated/" + key + "/k8s-objects/role.yaml", r)

    # trafficgen
    t = open("templates/k8s-objects/traffic-gen.yaml")
    template = Template(t.read())
    r = template.render(ns=productns, hostname=productns + ".tetrate.test.com", saName=service_account)
    t.close()
    save_file("generated/" + key + "/k8s-objects/traffic-gen.yaml", r)

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
        t = open("templates/tsb-objects/tenant.yaml")
        template = Template(t.read())
        r = template.render(
            orgName=conf.org,
            tenantName=tenant_name,
        )
        t.close()
        os.mkdir("generated")
        save_file("generated/tenant.yaml", r)
        install_bookinfo(conf, str(index))
    index += 1

if __name__ == "__main__":
    main()
