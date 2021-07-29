from common import *

def generate_tenant(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/tenant.yaml"
    generate_yaml(template, arguments, filepath)

def generate_workspace(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/workspace.yaml"
    generate_yaml(template, arguments, filepath)

def generate_groups(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/group.yaml"
    generate_yaml(template, arguments, filepath)

def generate_perm(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/perm.yaml"
    generate_yaml(template, arguments, filepath)

def generate_bridged_security(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/bridged/security.yaml"
    generate_yaml(template, arguments, filepath)

def generate_bridged_serviceroute(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/bridged/serviceroute.yaml"
    generate_yaml(template, arguments, filepath)

def generate_bridged_gateway(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/bridged/gateway.yaml"
    generate_yaml(template, arguments, filepath)

def generate_brigded_servicerouteeditor(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/bridged/servicerouteeditor.yaml"
    generate_yaml(template, arguments, filepath)

def generate_direct_reviews_vs(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/reviews-vs.yaml"
    generate_yaml(template, arguments, filepath)

def generate_direct_servicerouteeditor(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/direct/servicerouteeditor.yaml"
    generate_yaml(template, arguments, filepath)

def generate_direct_dr(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/dr.yaml"
    generate_yaml(template, arguments, filepath)

def generate_direct_vs(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/vs.yaml"
    generate_yaml(template, arguments, filepath)

def generate_direct_gateway(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/gw.yaml"
    generate_yaml(template, arguments, filepath)
