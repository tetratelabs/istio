import yaml
from jinja2 import Template
from dataclasses import dataclass
from typing import List
@dataclass
class bookinfo:
    replicas: int
    org: str
    cluster_name: str
    mode: str

def parse_config(yaml_dict):
    parsed_conf = []
    for config in yaml_dict["config"]:
        # context is not necessary, we can always fallback to current context
        mode = config["mode"]

        if mode != "direct" and mode != "bridged":
            print("Possible values for `mode` are `direct` and `bridged`, not ", mode)
            exit(1)

        conf = bookinfo(
            config["replicas"],
            config["organisation"],
            config["clusterName"],
            config["mode"],
        )
        parsed_conf.append(conf)
    return parsed_conf

def read_config_yaml(filename):
    with open(filename) as file:
        iop_config = yaml.load(file, Loader=yaml.FullLoader)
        return parse_config(iop_config)

def modify_gateway(filename, key):
    with open(filename) as file:
        template = Template(file.read())
        complete_yaml = template.render(
            gatewayName=key + "-gateway",
            hostname=key + ".k8s.local",
            secretName=key + "-credential",
        )
        return complete_yaml
