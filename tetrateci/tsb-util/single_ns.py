import os
from jinja2 import Template
import yaml
import base64
import copy
import argparse

script_path = os.path.dirname(os.path.realpath(__file__))

entries = {
    "name": "",
    "port": 8443,
    "hostname": "",
    "tls": {
        "mode": "SIMPLE",
        "secretName": "wilcard-credential",
    },
    "routing": {"rules": [{"route": {"host": ""}}]},
}


def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()


def create_cert():
    os.mkdir("cert")
    os.system(
        "openssl req -x509 -sha256 -nodes -days 365 \
        -newkey rsa:4096 -subj '/C=US/ST=CA/O=Tetrateio/CN=tetrate.test.com' \
        -keyout cert/tetrate.test.com.key -out cert/tetrate.test.com.crt"
    )

    os.system(
        "openssl req -out cert/wildcard.tetrate.test.com.csr -newkey rsa:2048 \
        -nodes -keyout cert/wildcard.tetrate.test.com.key \
        -subj '/CN=*.tetrate.test.com/O=bookinfo organization'"
    )

    os.system(
        "openssl x509 -req -sha256 -days 365 -CA cert/tetrate.test.com.crt \
        -CAkey cert/tetrate.test.com.key \
        -set_serial 0 -in cert/wildcard.tetrate.test.com.csr \
        -out cert/wildcard.tetrate.test.com.crt"
    )


def create_secret(ns, fname):
    secret_name = "wilcard-credential"
    hostname = "wildcard.tetrate.test.com"
    keyfile = open("cert/" + hostname + ".key")
    certfile = open("cert/" + hostname + ".crt")

    t = open(script_path + "/templates/k8s-objects/secret.yaml")
    template = Template(t.read())
    r = template.render(
        name=secret_name,
        ns=ns,
        crtData=base64.b64encode(certfile.read().encode("utf-8")).decode("utf-8"),
        keyData=base64.b64encode(keyfile.read().encode("utf-8")).decode("utf-8"),
    )
    t.close()

    f = open(fname, "w")
    f.write(r)
    f.close()

    keyfile.close()
    certfile.close()


def create_trafficgen_secret(ns, fname):
    secret_name = ns + "-ca-cert"
    certfile = open("cert/tetrate.test.com.crt")

    t = open(script_path + "/templates/k8s-objects/trafficgen-secret.yaml")
    template = Template(t.read())
    r = template.render(
        name=secret_name,
        ns=ns,
        data=base64.b64encode(certfile.read().encode("utf-8")).decode("utf-8"),
    )
    t.close()

    f = open(fname, "w")
    f.write(r)
    f.close()

    certfile.close()
    return secret_name


def install_httpbin(index, namespace):
    instance_name = "httpbin" + index
    t = open(script_path + "/templates/k8s-objects/httpbin.yaml")
    template = Template(t.read())
    r = template.render(namespace=namespace, name=instance_name)
    t.close()
    save_file("generated/k8s-objects/httpbin" + index + ".yaml", r)


def main():
    parser = argparse.ArgumentParser(description="Spin up httpbin instances")

    parser.add_argument(
        "--count", help="number of httpbin instances", type=int, required=True
    )
    parser.add_argument("--workspace", help="TSB workspace to be used", required=True)
    parser.add_argument(
        "--namespace", help="namespace to spin up all the pods", required=True
    )
    parser.add_argument("--tenant", help="TSB tenant to be used", required=True)
    parser.add_argument("--org", help="TSB org to be used", required=True)
    parser.add_argument("--group", help="TSB gateway group to be used", required=True)

    args = parser.parse_args()

    gateway_yaml = {
        "apiVersion": "gateway.tsb.tetrate.io/v2",
        "kind": "IngressGateway",
        "Metadata": {
            "organization": args.org,
            "name": "tsb-gateway",
            "group": args.group,
            "workspace": args.workspace,
            "tenant": args.tenant,
        },
        "spec": {
            "workloadSelector": {
                "namespace": args.namespace,
                "labels": {"app": "tsb-gateway-" + args.namespace},
            },
            "http": [],
        },
    }

    os.makedirs("generated/k8s-objects/", exist_ok=True)
    os.makedirs("generated/tsb-objects/", exist_ok=True)

    t = open(script_path + "/templates/k8s-objects/ingress.yaml")
    template = Template(t.read())
    r = template.render(ns=args.namespace)
    t.close()
    save_file("generated/k8s-objects/ingress.yaml", r)

    create_cert()
    create_secret(args.namespace, "generated/k8s-objects/secret.yaml")
    create_trafficgen_secret(
        args.namespace, "generated/k8s-objects/trafficgen-secret.yaml"
    )

    http_routes = []
    curl_calls = ""

    for i in range(args.count):
        install_httpbin(str(i), args.namespace)
        name = "httpbin" + str(i)
        entries["name"] = name
        hostname = name + ".tetrate.test.com"
        entries["hostname"] = hostname
        entries["routing"]["rules"][0]["route"]["host"] = (
            args.namespace + "/" + name + "." + args.namespace + ".svc.cluster.local"
        )
        http_routes.append(copy.deepcopy(entries))
        curl_calls += (
            "              curl https://"
            + hostname
            + " --connect-to "
            + hostname
            + ":443:$IP:$PORT --cacert /etc/bookinfo/bookinfo-ca.crt &>/dev/null\n"
        )
    gateway_yaml["spec"]["http"] = http_routes

    f = open("generated/tsb-objects/gateway.yaml", "w")
    yaml.dump(gateway_yaml, f)
    f.close()
    service_account = "httpbin-serviceaccount"
    t = open(script_path + "/templates/k8s-objects/role.yaml")
    template = Template(t.read())
    r = template.render(
        targetNS=args.namespace, clientNS=args.namespace, saName=service_account
    )
    t.close()
    save_file("generated/k8s-objects/role.yaml", r)

    t = open(script_path + "/templates/k8s-objects/traffic-gen-httpbin.yaml")
    template = Template(t.read())
    r = template.render(
        ns=args.namespace,
        saName=service_account,
        secretName=args.namespace + "-ca-cert",
        serviceName="tsb-gateway-" + args.namespace,
        content=curl_calls,
    )
    t.close()
    save_file("generated/k8s-objects/traffic-gen.yaml", r)


if __name__ == "__main__":
    main()
