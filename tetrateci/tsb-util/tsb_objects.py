import os
from jinja2 import Template

script_path = os.path.dirname(os.path.realpath(__file__))

def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()

def gen_tenant(org, tenant, filepath):
    t = open(script_path + "/templates/tsb-objects/tenant.yaml")
    template = Template(t.read())
    r = template.render(
        orgName=org,
        tenantName=tenant,
    )
    t.close()
    save_file(filepath, r)
