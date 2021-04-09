import yaml
from dataclasses import dataclass
from typing import List

@dataclass
class cluster_config:
    istio_tag: str
    instances: int
    context: str

def parse_config(yaml_dict):
    parsed_conf = []
    for config in yaml_dict['config']:
        # context is not necessary, we can always fallback to current context
        conf = cluster_config(config['istioTag'], config['instances'], config.get('context'))
        parsed_conf.append(conf)
    return parsed_conf

def read_config_yaml(filename):
    with open(filename) as file :
        iop_config = yaml.load(file, Loader=yaml.FullLoader)
        return parse_config(iop_config)

def modify_gateway(filename, hostname):
    with open(filename) as file :
        config = list(yaml.load_all(file, Loader=yaml.FullLoader))
        networking_config = config[0]
        networking_config['spec']['servers'][0]['hosts'][0] = hostname
        f = open(filename, 'w')
        yaml.dump_all(config, f)
