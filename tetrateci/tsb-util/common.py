import os
import sys
from jinja2 import Template
import time
import yaml

script_path = os.path.dirname(os.path.realpath(__file__))

def save_file(fname, content):
    with open(fname, "w") as f:
        f.write(content)

def generate_yaml(template, args, filepath):
    try:
        with open(template) as t:
            template = Template(t.read())
            r = template.render(**args)
            save_file(filepath, r)
    except Exception as e:
        print(e)
        print(f"Error while rendering template and writing to yaml file - {filepath}")
        sys.exit(1)

def default_folder():
    return int(time.time())

def dump_yaml(fname, content):
    with open(fname, "w") as f:
        yaml.dump(content, f)
