import yaml
from dataclasses import dataclass
from typing import List
from marshmallow_dataclass import class_schema

@dataclass
class replica:
    bridged: int
    direct: int
    tenant_id: int

@dataclass
class bookinfo:
    replicas: List[replica]
    cluster_name: str
    traffic_gen_ip: str

@dataclass
class fullconfig:
    org: str
    app: List[bookinfo]
    provider: str
    tctl_version: str

@dataclass
class htbn_multi_config:
    org: str
    app: List[bookinfo]

def read_config_yaml(filename):
    schema = class_schema(fullconfig)
    with open(filename) as file:
        config = yaml.load(file, Loader=yaml.SafeLoader)
        return schema().load(config)

def read_htbn_multi_config_yaml(filename):
    schema = class_schema(htbn_multi_config)
    with open(filename) as file:
        config = yaml.load(file, Loader=yaml.SafeLoader)
        return schema().load(config)
