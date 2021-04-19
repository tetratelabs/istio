import os

def create_root_cert():
    os.system("mkdir cert")
    os.system(
        "openssl req -x509 -sha256 -nodes -days 365 \
        -newkey rsa:2048 -subj '/O=k8s Inc./CN=k8s.local' \
        -keyout cert/k8s.local.key -out cert/k8s.local.crt"
    )

def create_private_key(ns):
    hostname = ns + ".k8s.local"
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
    hostname = ns + ".k8s.local"
    os.system(
        "openssl x509 -req -days 365 -CA cert/k8s.local.crt -CAkey cert/k8s.local.key -set_serial 0 -in cert/"
        + hostname
        + ".csr -out cert/"
        + hostname
        + ".crt"
    )

def create_secret(ns):
    print("avoiding secret creationg")
    """
    secret_name = ns + "-credential"
    hostname = ns + ".k8s.local"
    os.system(
        "kubectl create -n istio-system secret tls "
        + secret_name
        + " --key=cert/"
        + hostname
        + ".key --cert=cert/"
        + hostname
        + ".crt"
    )
    """
