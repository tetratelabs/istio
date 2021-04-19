import yaml
from jinja2 import Template
from dataclasses import dataclass
from typing import List
@dataclass
class productpage:
    context: str
    gateway_yaml: str
    virtualservice_yaml: str
    cluster_hostname: str

@dataclass
class reviewspage:
    context: str
    virtualservice_yaml: str
    destinationrules_yaml: str
    cluster_hostname: str

@dataclass
class detailspage:
    context: str
    cluster_hostname: str

@dataclass
class bookinfo:
    replicas: int
    context: str
    product: productpage
    reviews: reviewspage
    details: detailspage
    tenant_yaml: str
    workspace_yaml: str
    groups_yaml: str
    perm_yaml: str
    security_yaml: str
    org: str
    cluster_name: str

def parse_config(yaml_dict):
    parsed_conf = []
    for config in yaml_dict["config"]:
        # context is not necessary, we can always fallback to current context
        product = productpage(
            config["product"].get("context"),
            config["product"]["gatewayYaml"],
            config["product"]["virtualServiceYaml"],
            config["product"].get("clusterHostName")
        )
        reviews = reviewspage(
            config["reviews"].get("context"),
            config["reviews"]["virtualServiceYaml"],
            config["reviews"]["destinationRulesYaml"],
            config["reviews"].get("clusterHostName"),
        )
        details = config.get("details")
        if details is not None:
            details = detailspage(
                config["details"].get("context"),
                config["details"].get("clusterHostName"),
            )
        conf = bookinfo(
            config["replicas"],
            config.get("context"),
            product,
            reviews,
            details,
            config["tenantYaml"],
            config["workspaceYaml"],
            config["groupsYaml"],
            config["permYaml"],
            config["securityYaml"],
            config["organisation"],
            config["clusterName"]
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
