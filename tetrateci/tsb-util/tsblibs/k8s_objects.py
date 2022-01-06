from .common import script_path, generate_yaml

def generate_bookinfo(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/bookinfo.yaml"
    generate_yaml(template, arguments, filepath)

def generate_ingress(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/ingress.yaml"
    generate_yaml(template, arguments, filepath)

def generate_trafficgen_role(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/role.yaml"
    generate_yaml(template, arguments, filepath)

def generate_trafficgen(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/traffic-gen.yaml"
    generate_yaml(template, arguments, filepath)

def generate_bookinfo_namespaces(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/namespaces.yaml"
    generate_yaml(template, arguments, filepath)

def generate_httpbin(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/httpbin.yaml"
    generate_yaml(template, arguments, filepath)

def generate_tier1_ingress(arguments, filepath):
    template = f"{script_path}/templates/tier1/kube-tier1.yaml"
    generate_yaml(template, arguments, filepath)

def generate_tier1_ingress_namespace(arguments, filepath):
    template = f"{script_path}/templates/tier1/01namespace.yaml"
    generate_yaml(template, arguments, filepath)
