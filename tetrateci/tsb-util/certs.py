import os
import yaml
import base64

def create_root_cert():
    os.system("mkdir cert")
    os.system(
        "openssl req -x509 -sha256 -nodes -days 365 \
        -newkey rsa:2048 -subj '/O=k8s Inc./CN=tetrate.test.com' \
        -keyout cert/tetrate.test.com.key -out cert/tetrate.test.com.crt"
    )

def create_private_key(ns):
    hostname = ns + ".tetrate.test.com"
    os.system(
        "openssl req -out cert/"
        + hostname
        + ".csr -newkey rsa:2048 -nodes -keyout cert/"
        + hostname
        + '.key -subj "/CN='
        + hostname
        + '/O=bookinfo organization"'
    )

def create_cert(ns):
    hostname = ns + ".tetrate.test.com"
    os.system(
        "openssl x509 -req -days 365 -CA cert/tetrate.test.com.crt -CAkey cert/tetrate.test.com.key -set_serial 0 -in cert/"
        + hostname
        + ".csr -out cert/"
        + hostname
        + ".crt"
    )

def create_secret(ns, fname):
    secret_name = ns + "-credential"
    hostname = ns + ".tetrate.test.com"
    keyfile = open("cert/" + hostname + ".key")
    certfile = open("cert/" + hostname + ".crt")

    yamlcontent = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": secret_name, "namespace": ns},
        "type": "kubernetes.io/tls",
        "data": {
            # the data is abbreviated in this example
            "tls.crt": base64.b64encode(certfile.read().encode("utf-8")),
            "tls.key": base64.b64encode(keyfile.read().encode("utf-8")),
        },
    }

    f = open(fname, "w")
    yaml.safe_dump(yamlcontent, f)
    f.close()
    keyfile.close()
    certfile.close()
