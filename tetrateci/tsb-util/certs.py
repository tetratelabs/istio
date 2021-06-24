import os
import base64
from jinja2 import Template

script_path = os.path.dirname(os.path.realpath(__file__))

def create_root_cert(folder):
    os.makedirs(f"{folder}/cert/", exist_ok=True)
    if os.path.exists(f"{folder}/cert/tetrate.test.com.crt") and os.path.exists(
        f"{folder}/cert/tetrate.test.com.key"
    ):
        return

    os.system(
        f"openssl req -x509 -sha256 -nodes -days 365 \
        -newkey rsa:4096 -subj '/C=US/ST=CA/O=Tetrateio/CN=tetrate.test.com' \
        -keyout {folder}/cert/tetrate.test.com.key -out {folder}/cert/tetrate.test.com.crt"
    )

def create_private_key(ns, folder):
    hostname = ns + ".tetrate.test.com"
    if os.path.exists(f"{folder}/cert/{hostname}.csr") and os.path.exists(
        f"{folder}/cert/{hostname}.key"
    ):
        return
    os.system(
        f'openssl req -out {folder}/cert/{hostname}.csr -newkey rsa:2048 -nodes -keyout {folder}/cert/{hostname}.key \
            -subj "/CN={hostname} /O=bookinfo organization"'
    )

def create_cert(ns, folder):
    hostname = ns + ".tetrate.test.com"
    if os.path.exists(f"{folder}/cert/{hostname}.crt"):
        return
    os.system(
        f"openssl x509 -req -sha256 -days 365 -CA {folder}/cert/tetrate.test.com.crt -CAkey \
        {folder}/cert/tetrate.test.com.key -set_serial 0 -in {folder}/cert/{hostname}.csr -out {folder}/cert/{hostname}.crt"
    )

def create_secret(ns, fname, folder):
    secret_name = ns + "-credential"
    hostname = ns + ".tetrate.test.com"
    keyfile = open(f"{folder}/cert/{hostname}.key")
    certfile = open(f"{folder}/cert/{hostname}.crt")

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

def create_trafficgen_secret(ns, fname, folder):
    secret_name = ns + "-ca-cert"
    certfile = open(f"{folder}/cert/tetrate.test.com.crt")

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

def generate_wildcard_cert(folder):
    os.makedirs(f"{folder}/cert/", exist_ok=True)
    if not os.path.exists(f"{folder}/cert/tetrate.test.com.crt") or not os.path.exists(
        f"{folder}/cert/tetrate.test.com.key"
    ):
        os.system(
            f"openssl req -x509 -sha256 -nodes -days 365 \
        -newkey rsa:4096 -subj '/C=US/ST=CA/O=Tetrateio/CN=tetrate.test.com' \
        -keyout {folder}/cert/tetrate.test.com.key -out {folder}/cert/tetrate.test.com.crt"
        )

    if not os.path.exists(
        f"{folder}/cert/wildcard.tetrate.test.com.csr"
    ) or not os.path.exists(f"{folder}/cert/wildcard.tetrate.test.com.key"):
        os.system(
            f"openssl req -out {folder}/cert/wildcard.tetrate.test.com.csr -newkey rsa:2048 \
        -nodes -keyout {folder}/cert/wildcard.tetrate.test.com.key \
        -subj '/CN=*.tetrate.test.com/O=bookinfo organization'"
        )

    if not os.path.exists(f"{folder}/cert/wildcard.tetrate.test.com.crt"):
        os.system(
            f"openssl x509 -req -sha256 -days 365 -CA {folder}/cert/tetrate.test.com.crt \
        -CAkey {folder}/cert/tetrate.test.com.key \
        -set_serial 0 -in {folder}/cert/wildcard.tetrate.test.com.csr \
        -out {folder}/cert/wildcard.tetrate.test.com.crt"
        )

def create_wildcard_secret(ns, fname, folder):
    secret_name = "wildcard-credential"
    hostname = "wildcard.tetrate.test.com"
    keyfile = open(f"{folder}/cert/" + hostname + ".key")
    certfile = open(f"{folder}/cert/" + hostname + ".crt")

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

def create_trafficgen_wildcard_secret(ns, fname, folder):
    secret_name = ns + "-ca-cert"
    certfile = open(f"{folder}/cert/tetrate.test.com.crt")

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
