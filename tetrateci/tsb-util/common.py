import os
from jinja2 import Template

script_path = os.path.dirname(os.path.realpath(__file__))

def save_file(fname, content):
    with open(fname, "w") as f:
        f.write(content)

def generate_yaml(template, args, filepath):
    with open(template) as t:
        template = Template(t.read())
        r = template.render(**args)
        save_file(filepath, r)
