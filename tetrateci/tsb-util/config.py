import yaml
from dataclasses import dataclass
from typing import List

@dataclass
class bookinfo:
    replicas: int
    cluster_name: str
    mode: List[str]
    traffic_gen_ip: str
    org: str
    tenant_index: List[int]

@dataclass
class fullconfig:
    tenant_count: int
    org: str
    app: List[bookinfo]

def parse_config(yaml_dict):
    fullconf = fullconfig(yaml_dict["tenantCount"], yaml_dict["organisation"], [])
    parsed_conf = []
    for config in yaml_dict["config"]:
        # context is not necessary, we can always fallback to current context
        mode = config["mode"]

        if "direct" not in mode and "bridged" not in mode:
            print(
                "Possible values for `mode` array are `direct` and `bridged`, not ",
                mode,
            )
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
            config["clusterName"],
            config["mode"],
            "ExternalIP" if traffic_gen_ip == "external" else "InternalIP",
            yaml_dict[
                "organisation"
            ],  # keeping else need to refactor a lot, so probably in a later commit
            config["tenantIndex"],
        )
        parsed_conf.append(conf)
    fullconf.app = parsed_conf
    return fullconf

def read_config_yaml(filename):
    with open(filename) as file:
        iop_config = yaml.load(file, Loader=yaml.FullLoader)
        return parse_config(iop_config)
