import os
from jinja2 import Template

script_path = os.path.dirname(os.path.realpath(__file__))

def save_file(fname, content):
    with open(fname, "w") as f:
        f.write(content)

def gen_yaml(template, args, filepath):
    with open(template) as t:
        template = Template(t.read())
        r = template.render(**args)
        save_file(filepath, r)

def gen_tenant(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/tenant.yaml"
    gen_yaml(template, arguments, filepath)

def gen_workspace(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/workspace.yaml"
    gen_yaml(template, arguments, filepath)

def gen_groups(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/group.yaml"
    gen_yaml(template, arguments, filepath)

def gen_perm(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/perm.yaml"
    gen_yaml(template, arguments, filepath)

def gen_bridged_security(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/bridged/security.yaml"
    gen_yaml(template, arguments, filepath)

def gen_bridged_serviceroute(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/bridged/serviceroute.yaml"
    gen_yaml(template, arguments, filepath)

def gen_bridged_gateway(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/bridged/gateway.yaml"
    gen_yaml(template, arguments, filepath)

def gen_brigded_servicerouteeditor(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/bridged/servicerouteeditor.yaml"
    gen_yaml(template, arguments, filepath)

def gen_direct_reviews_vs(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/reviews-vs.yaml"
    gen_yaml(template, arguments, filepath)

def gen_direct_servicerouteeditor(arguments, filepath):
    template = f"{script_path}/templates/k8s-objects/direct/servicerouteeditor.yaml"
    gen_yaml(template, arguments, filepath)

def gen_direct_dr(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/dr.yaml"
    gen_yaml(template, arguments, filepath)

def gen_direct_vs(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/vs.yaml"
    gen_yaml(template, arguments, filepath)

def gen_direct_gateway(arguments, filepath):
    template = f"{script_path}/templates/tsb-objects/direct/gw.yaml"
    gen_yaml(template, arguments, filepath)
