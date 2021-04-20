import urllib.request
import os, sys, shutil, platform
import argparse
import config
import certs
from subprocess import PIPE, Popen
from jinja2 import Template

# https://stackoverflow.com/a/23796709
def cmdline(command):
    process = Popen(args=command, stdout=PIPE, shell=True)
    return process.communicate()[0]

def print_cmdline(command):
    #print(str(cmdline(command), "utf-8"), end="")
    pass

def apply_from_stdin(ns, yaml):
    cmd = "cat << EOF | kubectl apply " + " -n " + ns + " -f - \n" + yaml + "\nEOF\n"
    print_cmdline(cmd)

def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()

cleanup_script = ""

def create_namespace(ns):
    global cleanup_script
    print("Create Namespace : " + ns)
    print_cmdline("kubectl create ns " + ns)
    print_cmdline("kubectl label namespace " + ns + " istio-injection=enabled")
    cleanup_script += "kubectl delete ns " + ns + "\n"

def install_bookinfo(conf, default_context, tenant_index):
    global cleanup_script

    base_cmd = "kubectl apply -f /tmp/bookinfo.yaml -n "
    tenant_name = "bookinfo-tenant-" + tenant_index

    i = 0

    while i < conf.replicas * 3:
        print("Installing Bookinfo")
        key = str(int(i/3))
        workspace_name = "bookinfo-ws-" + key
        os.mkdir("genned/" + key)
        os.mkdir("genned/" + key + "/k8s-objects")
        os.mkdir("genned/" + key + "/tsb-objects")

        productns = "bookinfo-b" + key + "-t" + tenant_index + "-front"
        detailsns = "bookinfo-b" + key + "-t" + tenant_index + "-mid"
        reviewsns = "bookinfo-b" + key + "-t" + tenant_index + "-back"

        t = open("k8s-objects/bookinfo.yaml")
        template = Template(t.read())
        r = template.render(
            reviewsns=reviewsns,
            detailsns=detailsns,
            productns=productns,
        )
        t.close()
        save_file("genned/"+key+"/k8s-objects/bookinfo.yaml", r)

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
            clusterName=conf.cluster_name
        )
        t.close()
        save_file("genned/"+key+"/tsb-objects/workspaces.yaml", r)
        apply_from_stdin("tetrate", r)

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
            clusterName=conf.cluster_name
        )
        t.close()
        save_file("genned/"+key+"/tsb-objects/groups.yaml", r)
        apply_from_stdin("tetrate", r)

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
        save_file("genned/"+key+"/tsb-objects/perm.yaml", r)
        apply_from_stdin("tetrate", r)

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
        save_file("genned/"+key+"/tsb-objects/security.yaml", r)
        apply_from_stdin("tetrate", r)

        if conf.details is not None and conf.details.context is not None:
            switch_context(conf.details.context)
        else:
            switch_context(default_context)

        create_namespace(detailsns)

        print("Installing details")
        print_cmdline(base_cmd + detailsns + " -l account=details")
        print_cmdline(base_cmd + detailsns + " -l app=details")

        i += 1

        if conf.reviews.context is not None:
            switch_context(conf.reviews.context)
        else:
            switch_context(default_context)

        create_namespace(reviewsns)

        print("Installing reviews and ratings")
        print_cmdline(base_cmd + reviewsns + " -l account=reviews")
        print_cmdline(base_cmd + reviewsns + " -l app=reviews")
        print_cmdline(base_cmd + reviewsns + " -l account=ratings")
        print_cmdline(base_cmd + reviewsns + " -l app=ratings")

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
        save_file("genned/"+key+"/tsb-objects/serviceroute.yaml", r)
        t.close()
        apply_from_stdin(reviewsns, r)

        print_cmdline(
            "kubectl apply -f " + conf.reviews.destinationrules_yaml + " -n " + reviewsns
        )

        i += 1

        if conf.product.context is not None:
            switch_context(conf.product.context)
        else:
            switch_context(default_context)

        create_namespace(productns)

        print("Installing productpage")
        print_cmdline(base_cmd + productns + " -l account=productpage")
        print_cmdline(base_cmd + productns + " -l app=productpage")

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
        details_env = "DETAILS_HOSTNAME=details." + detailsns + details_domain
        reviews_env = "REVIEWS_HOSTNAME=reviews." + reviewsns + reviews_domain
        ratings_env = "RATINGS_HOSTNAME=ratings." + reviewsns + reviews_domain

        cmd = (
            "kubectl set env deployments productpage-v1 -n "
            + productns
            + " "
            + ratings_env
            + " "
            + details_env
            + " "
            + reviews_env
        )
        print_cmdline(cmd)

        certs.create_private_key(productns)
        certs.create_cert(productns)
        certs.create_secret(productns, "genned/"+key+"/k8s-objects/secret.yaml")

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
        apply_from_stdin(productns, r)
        save_file("genned/"+key+"/tsb-objects/gateway.yaml", r)

        product_vs = conf.product.virtualservice_yaml
        virtual_service = config.modify_gateway(product_vs, productns)
        cmd = (
            "cat << EOF | kubectl apply "
            + " -n "
            + productns
            + " -f - \n"
            + virtual_service
            + "\nEOF\n"
        )
        print_cmdline(cmd)

        # ingress
        t = open("./k8s-objects/ingress.yaml")
        template = Template(t.read())
        r = template.render(
            ns=productns,
        )
        t.close()
        apply_from_stdin(productns, r)
        save_file("genned/"+key+"/k8s-objects/ingress.yaml", r)

        # trafficgen
        t = open("./k8s-objects/traffic-gen.yaml")
        template = Template(t.read())
        r = template.render(
            ns=productns,
            hostname=productns+".k8s.local"
        )
        t.close()
        apply_from_stdin(productns, r)
        save_file("genned/"+key+"/k8s-objects/traffic-gen.yaml", r)

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
        apply_from_stdin("tetrate", r)  # namespaces
        install_bookinfo(conf, default_context, str(index))
    index += 1

    f = open("./cleanup.sh", "w")
    #f.write(cleanup_script)
    f.close()

    print("Run `bash cleanup.sh` for cleaning up all the resources including istio.")

if __name__ == "__main__":
    main()
