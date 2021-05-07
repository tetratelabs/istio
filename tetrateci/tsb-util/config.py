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
    traffic_gen_ip: str

def parse_config(yaml_dict):
    parsed_conf = []
    for config in yaml_dict["config"]:
        # context is not necessary, we can always fallback to current context
        mode = config["mode"]

        if mode != "direct" and mode != "bridged":
            print("Possible values for `mode` are `direct` and `bridged`, not ", mode)
            exit(1)

        traffic_gen_ip = config["trafficGenIPType"]

        if traffic_gen_ip != "external" and traffic_gen_ip != "internal":
            print(
                "Possible values for `trafficGenIPType` are `external` and `internal`, not ",
                traffic_gen_ip,
            )
            exit(1)

        conf = bookinfo(
            config["replicas"],
            config["organisation"],
            config["clusterName"],
            config["mode"],
            "ExternalIP" if traffic_gen_ip == "external" else "InternalIP",
        )
        parsed_conf.append(conf)
    return parsed_conf

def read_config_yaml(filename):
    with open(filename) as file:
        iop_config = yaml.load(file, Loader=yaml.FullLoader)
        return parse_config(iop_config)
