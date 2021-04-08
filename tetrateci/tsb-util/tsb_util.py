import urllib.request
import os, sys, shutil, platform
import argparse
import config

def install_istio(tag) :
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
    os.system("/tmp/" + folder_name + "/bin/istioctl install -y")

def install_bookinfo(bookinfo_instances, istio_tag):
    folder_name = "istio-" + istio_tag
    services = ["productpage", "ratings", "details", "reviews"]
    base_cmd = "kubectl apply -f /tmp/" + folder_name + "/samples/bookinfo/platform/kube/bookinfo.yaml -n "
    i = 0
    while i < bookinfo_instances:

        if services[i%4] == "reviews":

            j = 1

            while i < bookinfo_instances and j <= 3:
                print("Installing Bookinfo")
                ver = str(j)

                ns = "bookinfo" + str(i)
                print("Create Namespace : " + ns)
                os.system("kubectl create ns " + ns)
                os.system("kubectl label namespace " + ns + " istio-injection=enabled")

                print("Installing reviews-v" + ver)
                os.system(base_cmd + ns + " -l account=reviews")
                os.system(base_cmd + ns + " -l app=reviews,version=v" + ver)
                os.system(base_cmd + ns + " -l service=reviews")
                
                j += 1
                i += 1

                print("Bookinfo installed\n")

            continue


        print("Installing Bookinfo")

        ns = "bookinfo" + str(i)
        print("Create Namespace : " + ns)
        os.system("kubectl create ns " + ns)
        os.system("kubectl label namespace " + ns + " istio-injection=enabled")

        print("Installing " + services[i%4])
        os.system(base_cmd + ns + " -l account=" + services[i%4])
        os.system(base_cmd + ns + " -l app=" + services[i%4])

        i += 1

        print("Bookinfo installed\n")

def main():
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

        if not args.noistio:
            install_istio(conf.istio_tag)

        install_bookinfo(conf.instances, conf.istio_tag)

if __name__ == "__main__":
    main()
