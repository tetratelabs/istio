import urllib.request
import os, sys, shutil, platform
import argparse
import config

cleanup_script = ""

def install_istio(tag) :
    global cleanup_script

    client_os = "linux"

    if platform.system() == "Darwin":
        client_os = "osx"
    elif platform.system() == "Windows":
        print("Try Mac OS or Linux.")
        sys.exit()

    print("Arch is set to amd64 no other architectures are supported for now.")

    arch = "amd64"
    archive_type = ".tar.gz"

    base_url = "https://bintray.com/api/ui/download/tetrate/getistio/istio"
    download_url = base_url + "-" + tag + "-" + client_os + "-" + arch + archive_type

    print("Fetching istio from " + download_url)
    temp_path = "/tmp/istio"
    urllib.request.urlretrieve(download_url, temp_path + archive_type)

    print("Upacking archive : " + temp_path + archive_type)
    shutil.unpack_archive(temp_path + archive_type, "/tmp")

    folder_name = "istio-" + tag
    command = "/tmp/" + folder_name + "/bin/istioctl install -y"
    print("Installing istio with :" + command)
    os.system(command)
    uninstall_command = "/tmp/" + folder_name + "/bin/istioctl x uninstall --purge -y"
    cleanup_script += uninstall_command + "\n"

def install_bookinfo(bookinfo_instances, istio_tag):
    global cleanup_script
    bookinfo_instances *= 4
    folder_name = "istio-" + istio_tag
    services = ["ratings", "details", "reviews", "productpage"]
    base_cmd = "kubectl apply -f /tmp/" + folder_name + "/samples/bookinfo/platform/kube/bookinfo.yaml -n "

    virtual_service = "/tmp/" + folder_name + "/samples/bookinfo/networking/virtual-service-reviews-50-v3.yaml"
    config.modify_virtual_service(virtual_service)

    i = 0

    while i < bookinfo_instances:
        print("Installing Bookinfo")

        ns = "bookinfo" + str(i)
        print("Create Namespace : " + ns)
        os.system("kubectl create ns " + ns)
        os.system("kubectl label namespace " + ns + " istio-injection=enabled")
        cleanup_script += "kubectl delete ns " + ns + "\n"

        print("Installing " + services[i%4])
        os.system(base_cmd + ns + " -l account=" + services[i%4])
        os.system(base_cmd + ns + " -l app=" + services[i%4])

        if services[i%4] == "productpage":
            svc_domain = ".svc.cluster.local"
            ratings_env = "RATINGS_HOSTNAME=ratings.bookinfo" + str(i-3) + svc_domain
            details_env = "DETAILS_HOSTNAME=details.bookinfo"+ str(i-2) + svc_domain
            reviews_env = "REVIEWS_HOSTNAME=reviews.bookinfo"+ str(i-1) + svc_domain
            cmd = "kubectl set env deployments productpage-v1 -n " + ns + " " + ratings_env + " " + details_env + " " + reviews_env
            os.system(cmd)
            gateway_file = "/tmp/"+ folder_name +"/samples/bookinfo/networking/bookinfo-gateway.yaml"
            hostname = ns + ".k8s.local"
            config.modify_gateway(gateway_file, hostname)
            cmd = "kubectl apply -f " + gateway_file + " -n " + ns
            os.system(cmd)

        if services[i%4] == "reviews":
            cmd = "kubectl apply -f " + virtual_service + " -n " + ns
            os.system(cmd)
            cmd = "kubectl apply -f /tmp/" + folder_name + "/samples/bookinfo/networking/destination-rule-reviews.yaml -n " + ns
            os.system(cmd)

        i += 1

        print("Bookinfo installed\n")

def main():
    global cleanup_script

    parser = argparse.ArgumentParser(description="Spin up bookinfo instances")

    parser.add_argument("--noistio", help="do not install istio on the cluster", action="store_true")
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
            os.system(cmd)
            cleanup_script += cmd + "\n"

        if not args.noistio:
            install_istio(conf.istio_tag)

        install_bookinfo(conf.instances, conf.istio_tag)

    f = open("./cleanup.sh", "w")
    f.write(cleanup_script)
    f.close()

    print("Run `bash cleanup.sh` for cleaning up all the resources including istio.")

if __name__ == "__main__":
    main()
