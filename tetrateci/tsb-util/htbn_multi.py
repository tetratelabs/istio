import os
import sys
import argparse
import config
import certs
import tsb_objects, k8s_objects, common
from marshmallow_dataclass import marshmallow
import shutil

def gen_common_tsb_objects(arguments, key, folder):
    # workspace
    tsb_objects.generate_workspace(
        arguments,
        f"{folder}/tsb-objects/{key}/workspaces.yaml",
    )
    # groups
    tsb_objects.generate_groups(arguments, f"{folder}/tsb-objects/{key}/groups.yaml")
    # perm
    tsb_objects.generate_perm(arguments, f"{folder}/tsb-objects/{key}/perm.yaml")

def gen_bridge_specific_objects(arguments, key, folder):
    os.makedirs(f"{folder}/tsb-objects/{key}/bridged", exist_ok=True)
    os.makedirs(f"{folder}/k8s-objects/{key}", exist_ok=True)

    tsb_objects.generate_bridged_security(
        arguments, f"{folder}/tsb-objects/{key}/bridged/security.yaml"
    )

    tsb_objects.generate_bridged_gateway(
        arguments, f"{folder}/tsb-objects/{key}/bridged/gateway.yaml"
    )

def gen_direct_specific_objects(arguments, key, folder):
    os.makedirs(f"{folder}/tsb-objects/{key}/direct", exist_ok=True)
    os.makedirs(f"{folder}/k8s-objects/{key}", exist_ok=True)

    # virtual service for product page
    tsb_objects.generate_direct_vs(
        arguments, f"{folder}/tsb-objects/{key}/direct/virtualservice.yaml"
    )
    # gateway
    tsb_objects.generate_direct_gateway(
        arguments, f"{folder}/tsb-objects/{key}/direct/gateway.yaml"
    )

def install_httpbin(
    conf,
    org,
    folder=common.default_folder(),
):
    count = 0
    for replica in conf.replicas:
        i = 0

        modes_list = ["bridged"] * replica.bridged + ["direct"] * replica.direct

        while i < (replica.bridged + replica.direct):
            print("Installing Httpbin")
            key = conf.cluster_name + "-" + str(count)

            current_mode = modes_list[i]

            tenant_id = str(replica.tenant_id)

            mode = "d" if current_mode == "direct" else "b"
            workspace_name = f"htbnt{tenant_id}ws{count}"

            os.makedirs(f"{folder}/k8s-objects/{key}", exist_ok=True)
            os.makedirs(f"{folder}/tsb-objects/{key}", exist_ok=True)

            namespace = f"t{tenant_id}w{count}{conf.cluster_name}htbnn{mode}0f"
            gateway_group = f"htbnt{tenant_id}w{count}{mode}gg0"
            traffic_group = f"htbnt{tenant_id}w{count}{mode}tg0"
            security_group = f"htbnt{tenant_id}w{count}{mode}sg0"

            arguments = {
                "tenantName": f"tenant{tenant_id}",
                "orgName": org,
                "workspaceName": workspace_name,
                "clusterName": conf.cluster_name,
                "gatewayGroupName": gateway_group,
                "trafficGroupName": traffic_group,
                "securityGroupName": security_group,
                "mode": current_mode.upper(),
                "securitySettingName": "httpbin-security-setting",
                "gatewayName": f"{namespace}-gateway",
                "hostname": f"{namespace}.tetrate.test.com",
                "gwSecretName": f"{namespace}-credential",
                "productHostFQDN": f"httpbin.{namespace}.svc.cluster.local",
                "destinationFQDN": f"httpbin.{namespace}.svc.cluster.local",
                "virtualserviceName": "httpbin-virtualservice",
                "ipType": "InternalIP"
                if conf.traffic_gen_ip == "internal"
                else "ExternalIP",
                "name": "httpbin",
                "namespace": namespace,
            }

            k8s_objects.generate_httpbin(
                arguments, f"{folder}/k8s-objects/{key}/httpbin.yaml"
            )

            gen_common_tsb_objects(arguments, key, folder)

            namespace_yaml = {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {
                    "labels": {"istio-injection": "enabled"},
                    "name": namespace,
                },
            }

            common.dump_yaml(
                f"{folder}/k8s-objects/{key}/01namespace.yaml", namespace_yaml
            )

            if current_mode == "bridged":
                gen_bridge_specific_objects(arguments, key, folder)
            else:
                gen_direct_specific_objects(arguments, key, folder)

            k8s_objects.generate_ingress(
                arguments, f"{folder}/k8s-objects/{key}/ingress.yaml"
            )

            certs.create_private_key(namespace, folder)
            certs.create_cert(namespace, folder)
            certs.create_secret(
                namespace, f"{folder}/k8s-objects/{key}/secret.yaml", folder
            )

            arguments["secretName"] = certs.create_trafficgen_secret(
                namespace,
                f"{folder}/k8s-objects/{key}/{namespace}-secret.yaml",
                folder,
            )

            k8s_objects.generate_trafficgen_role(
                arguments, f"{folder}/k8s-objects/{key}/role.yaml"
            )

            k8s_objects.generate_trafficgen(
                arguments, f"{folder}/k8s-objects/{key}/traffic-gen.yaml"
            )

            print("Httpbin installed\n")
            i += 1
            count += 1
    return count

def main():
    parser = argparse.ArgumentParser(description="Spin up httpbin instances")

    parser.add_argument(
        "--config",
        help="the yaml configuration for the httpbin instances",
        required=True,
    )

    parser.add_argument(
        "--folder",
        help="folder where the generated files would be stored",
        default=common.default_folder(),
    )
    args = parser.parse_args()
    folder = args.folder
    os.makedirs(f"{folder}", exist_ok=True)
    shutil.copy2(args.config, f"{folder}/config.yaml")
    try:
        configs = config.read_htbn_multi_config_yaml(args.config)
    except marshmallow.exceptions.ValidationError as e:
        print("Validation errors in the configuration file.")
        print(e)
        sys.exit(1)
    except Exception as e:
        print(e)
        print("Unable to read the config file.")
        sys.exit(1)

    try:
        certs.create_root_cert(folder)
    except Exception as e:
        print(e)
        print("Error while generating certs")
        sys.exit(1)

    try:
        tenant_set = set()
        cluster_list = []

        for appconfig in configs.app:
            for replica in appconfig.replicas:
                tenant_set.add(replica.tenant_id)
            if appconfig.cluster_name in cluster_list:
                print("Multiple entries for the same cluster found, please fix.")
            cluster_list.append(appconfig.cluster_name)

        for tenant_id in tenant_set:
            tenant_name = f"tenant{tenant_id}"
            tsb_objects.generate_tenant(
                {"orgName": configs.org, "tenantName": tenant_name},
                f"{folder}/tenant{tenant_id}.yaml",
            )

        for appconfig in configs.app:
            install_httpbin(
                appconfig,
                configs.org,
                folder,
            )
    except Exception as e:
        print(e)
        print("Unknown error occurred while installing httpbin.")
        sys.exit(1)

if __name__ == "__main__":
    main()
