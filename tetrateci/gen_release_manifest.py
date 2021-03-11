#!/usr/bin/python3

import yaml, sys, os

if len(sys.argv) != 3:
    print("Usage ./gen_release_manifest.py source_yaml distination_folder")
    exit(1)

print("Reading arguments")
source_yaml = sys.argv[1]
destination_folder = sys.argv[2]

print("Reading environment variables")
hub = os.environ.get("HUB")
tag = os.environ.get("TAG")
branch = os.environ.get("BRANCH")

print("HUB: ", hub)
print("TAG: ", tag)
print("BRANCH: ", branch)

with open(source_yaml, "r") as file :
    print("Loading source yaml: ", source_yaml)
    manifest = yaml.load(file, Loader=yaml.FullLoader)
    manifest["ignoreVulnerability"] = False
    manifest["docker"] = hub
    manifest["version"] = tag
    manifest["dependencies"]["istio"] = {"localpath" : "./istio"}
    manifest["dependencies"]["client-go"]["branch"] = branch
    manifest["dependencies"]["gogo-genproto"]["branch"] = branch
    manifest["dependencies"]["tools"]["branch"] = branch
    manifest['outputs'] = ["docker"]
    f = open(os.path.join(destination_folder, "manifest.docker.yaml"), 'w')
    yaml.dump(manifest, f)
    print(manifest)
    manifest['outputs'] = ["archives"]
    print(manifest)
    f = open(os.path.join(destination_folder, "manifest.archive.yaml"), 'w')
    yaml.dump(manifest, f)

