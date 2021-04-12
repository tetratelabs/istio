import yaml
from dataclasses import dataclass
from typing import List
@dataclass
class productpage:
    context: str
    gateway_yaml: str

@dataclass
class reviewspage:
    context: str
    virtualservice_yaml: str
    destinationrules_yaml: str

@dataclass
class detailspage:
    context: str

@dataclass
class bookinfo:
    replicas: int
    context: str
    product: productpage
    reviews: reviewspage
    details: detailspage

def parse_config(yaml_dict):
    parsed_conf = []
    for config in yaml_dict["config"]:
        # context is not necessary, we can always fallback to current context
        product = productpage(
            config["product"].get("context"), config["product"]["gatewayYaml"]
        )
        reviews = reviewspage(
            config["reviews"].get("context"),
            config["reviews"]["virtualServiceYaml"],
            config["reviews"]["destinationRulesYaml"],
        )
        details = config.get("details")
        if details is not None:
            details = detailspage(config["details"].get("context"))
        conf = bookinfo(
            config["replicas"], config.get("context"), product, reviews, details
        )
        parsed_conf.append(conf)
    return parsed_conf

def read_config_yaml(filename):
    with open(filename) as file:
        iop_config = yaml.load(file, Loader=yaml.FullLoader)
        return parse_config(iop_config)

def modify_gateway(filename, hostname):
    with open(filename) as file:
        config = list(yaml.load_all(file, Loader=yaml.FullLoader))
        networking_config = config[0]
        networking_config["spec"]["servers"][0]["hosts"][0] = hostname
        f = open(filename, "w")
        yaml.dump_all(config, f)
