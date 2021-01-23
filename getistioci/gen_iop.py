#!/usr/bin/python

import yaml

extra = {'cni' : {'cniBinDir' : '/home/kubernetes/bin', 'excludeNamespaces' : ['istio-system', 'kube-system']}}

with open(r'./tests/integration/iop-integration-test-defaults.yaml') as file :
    iop_config = yaml.load(file, Loader=yaml.FullLoader)
    iop_config['spec']['values'].update(extra)
    f = open(r'./getistioci/iop-gke-integration.yml', 'w')
    yaml.dump(iop_config, f)