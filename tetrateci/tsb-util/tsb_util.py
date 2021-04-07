import urllib.request
import os, sys, shutil, platform

tag = "1.9.2-tetrate-v0"

client_os = "linux"

if platform.system() == "Darwin":
    os = "osx"
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

bookinfo_instances = 2

print("Installing BookInfo")

for i in range(bookinfo_instances):
    ns = "bookinfo" + str(i)
    print("Create Namespace : " + ns)
    os.system("kubectl create ns " + ns)

    print("Installing bookinfo")
    os.system("kubectl apply -f /tmp/" + folder_name + "/samples/bookinfo/platform/kube/bookinfo.yaml -n " + ns)
    print("Bookinfo installed")
