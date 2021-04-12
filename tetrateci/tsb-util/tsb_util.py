import urllib.request
import os, sys, shutil, platform
import argparse
import config
from subprocess import PIPE, Popen

# https://stackoverflow.com/a/23796709
def cmdline(command):
    process = Popen(args=command, stdout=PIPE, shell=True)
    return process.communicate()[0]

def print_cmdline(command):
    print(str(cmdline(command), "utf-8"), end="")

cleanup_script = ""

def install_bookinfo(conf):
    global cleanup_script

    download_url = "https://raw.githubusercontent.com/istio/istio/master/samples/bookinfo/platform/kube/bookinfo.yaml"
    urllib.request.urlretrieve(download_url, "/tmp/bookinfo.yaml")
    base_cmd = "kubectl apply -f /tmp/bookinfo.yaml -n "

    # virtual_service = "/tmp/" + folder_name + "/samples/bookinfo/networking/virtual-service-reviews-50-v3.yaml"
    # config.modify_virtual_service(virtual_service)

    i = 0

    while i < conf.replicas * 3:
        print("Installing Bookinfo")

        ns = "bookinfo" + str(i)
        print("Create Namespace : " + ns)
        print_cmdline("kubectl create ns " + ns)
        print_cmdline("kubectl label namespace " + ns + " istio-injection=enabled")
        cleanup_script += "kubectl delete ns " + ns + "\n"

        print("Installing details")
        print_cmdline(base_cmd + ns + " -l account=details")
        print_cmdline(base_cmd + ns + " -l app=details")

        i += 1

        ns = "bookinfo" + str(i)
        print("Create Namespace : " + ns)
        print_cmdline("kubectl create ns " + ns)
        print_cmdline("kubectl label namespace " + ns + " istio-injection=enabled")
        cleanup_script += "kubectl delete ns " + ns + "\n"

        print("Installing reviews and ratings")
        print_cmdline(base_cmd + ns + " -l account=reviews")
        print_cmdline(base_cmd + ns + " -l app=reviews")
        print_cmdline(base_cmd + ns + " -l account=ratings")
        print_cmdline(base_cmd + ns + " -l app=ratings")
        print_cmdline(
            "kubectl apply -f " + conf.reviews.virtualservice_yaml + " -n " + ns
        )
        print_cmdline(
            "kubectl apply -f " + conf.reviews.destinationrules_yaml + " -n " + ns
        )

        i += 1

        ns = "bookinfo" + str(i)
        print("Create Namespace : " + ns)
        print_cmdline("kubectl create ns " + ns)
        print_cmdline("kubectl label namespace " + ns + " istio-injection=enabled")
        cleanup_script += "kubectl delete ns " + ns + "\n"
        print("Installing productpage")
        print_cmdline(base_cmd + ns + " -l account=productpage")
        print_cmdline(base_cmd + ns + " -l app=productpage")

        svc_domain = ".svc.cluster.local"
        details_env = "DETAILS_HOSTNAME=details.bookinfo" + str(i - 2) + svc_domain
        reviews_env = "REVIEWS_HOSTNAME=reviews.bookinfo" + str(i - 1) + svc_domain
        ratings_env = "RATINGS_HOSTNAME=ratings.bookinfo" + str(i - 1) + svc_domain
        cmd = (
            "kubectl set env deployments productpage-v1 -n "
            + ns
            + " "
            + ratings_env
            + " "
            + details_env
            + " "
            + reviews_env
        )
        print_cmdline(cmd)

        gateway_file = conf.product.gateway_yaml
        hostname = ns + ".k8s.local"
        config.modify_gateway(gateway_file, hostname)
        cmd = "kubectl apply -f " + gateway_file + " -n " + ns
        print_cmdline(cmd)

        i += 1

        print("Bookinfo installed\n")

def main():
    global cleanup_script

    parser = argparse.ArgumentParser(description="Spin up bookinfo instances")

    parser.add_argument("--config", help="the istio version tag to be installed")
    args = parser.parse_args()

    if args.config is None:
        print("Pass in the config file with the `--config` flag")
        sys.exit(1)

    configs = config.read_config_yaml(args.config)

    for conf in configs:
        if conf.context is not None:
            cmd = "kubectl config use-context " + conf.context
            print("Switching Context | Running: " + cmd)
            print_cmdline(cmd)
            cleanup_script += cmd + "\n"

        install_bookinfo(conf)

    f = open("./cleanup.sh", "w")
    f.write(cleanup_script)
    f.close()

    print("Run `bash cleanup.sh` for cleaning up all the resources including istio.")

if __name__ == "__main__":
    main()
