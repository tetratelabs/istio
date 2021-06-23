from common import *

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
