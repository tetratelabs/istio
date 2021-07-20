#!/usr/bin/python3

import sys, os

version_matrix = {
    "1.7": {"1.16", "1.17", "1.18"},
    "1.8": {"1.16", "1.17", "1.18", "1.19"},
    "1.9": {"1.17", "1.18", "1.19", "1.20"},
    "1.10": {"1.18", "1.19", "1.20", "1.21"},
}

istio_ver = os.environ.get("ISTIO_MINOR_VER")
k8s_ver = os.environ.get("K8S_VERSION")

print("Istio Version : ", istio_ver)
print("Kubernetes Version : ", k8s_ver)

if istio_ver in version_matrix:
    if k8s_ver in version_matrix[istio_ver]:
        print("Version matched!!")
        sys.exit(1)

print("Version not matched!!")
sys.exit(0)
