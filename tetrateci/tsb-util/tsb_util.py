import os
import sys
import argparse
import config
import certs
import tsb_objects, k8s_objects

def gen_common_tsb_objects(arguments, key):
    # workspace
    tsb_objects.generate_workspace(
        arguments,
        f"generated/tsb-objects/{key}/workspaces.yaml",
    )
    # groups
    tsb_objects.generate_groups(arguments, f"generated/tsb-objects/{key}/groups.yaml")
    # perm
    tsb_objects.generate_perm(arguments, f"generated/tsb-objects/{key}/perm.yaml")

def gen_bridge_specific_objects(
    arguments,
    key,
):
    os.makedirs("generated/tsb-objects/" + key + "/bridged", exist_ok=True)
    os.makedirs("generated/k8s-objects/" + key + "/bridged", exist_ok=True)

    tsb_objects.generate_bridged_security(
        arguments, f"generated/tsb-objects/{key}/bridged/security.yaml"
    )
    tsb_objects.generate_bridged_serviceroute(
        arguments, f"generated/tsb-objects/{key}/bridged/serviceroute.yaml"
    )
    tsb_objects.generate_bridged_gateway(
        arguments, f"generated/tsb-objects/{key}/bridged/gateway.yaml"
    )
    tsb_objects.generate_brigded_servicerouteeditor(
        arguments, f"generated/tsb-k8s-objects/{key}/servicerouteeditor.yaml"
    )

def gen_direct_specific_objects(
    arguments,
    key,
):
    os.makedirs(f"generated/tsb-objects/{key}/direct", exist_ok=True)
    os.makedirs(f"generated/k8s-objects/{key}/direct", exist_ok=True)

    # reviews virtual service
    tsb_objects.generate_direct_reviews_vs(
        arguments, f"generated/tsb-objects/{key}/direct/reviews_vs.yaml"
    )
    tsb_objects.generate_direct_servicerouteeditor(
        arguments, f"generated/tsb-k8s-objects/{key}/servicerouteeditor.yaml"
    )
    # destination rules
    tsb_objects.generate_direct_dr(
        arguments, f"generated/tsb-objects/{key}/direct/destinationrule.yaml"
    )
    # virtual service for product page
    tsb_objects.generate_direct_vs(
        arguments, f"generated/tsb-objects/{key}/direct/virtualservice.yaml"
    )
    # gateway
    tsb_objects.generate_direct_gateway(
        arguments, f"generated/tsb-objects/{key}/direct/gateway.yaml"
    )

def install_bookinfo(conf, password, org, provider="others", tctl_ver="1.2.0"):
    count = 0
    for replica in conf.replicas:
        i = 0

        modes_list = ["bridged"] * replica.bridged + ["direct"] * replica.direct

        while i < (replica.bridged + replica.direct):
            print("Installing Bookinfo")
            key = conf.cluster_name + "-" + str(count)

            current_mode = modes_list[i]

            tenant_id = str(replica.tenant_id)

            mode = "d" if current_mode == "direct" else "b"
            workspace_name = f"bkift{tenant_id}ws{count}"

            os.makedirs("generated/k8s-objects/" + key, exist_ok=True)
            os.makedirs("generated/tsb-objects/" + key, exist_ok=True)
            os.makedirs("generated/tsb-k8s-objects/" + key, exist_ok=True)

            namespaces = {
                "product": f"t{tenant_id}w{count}{conf.cluster_name}bkifn{mode}f0",
                "ratings": f"t{tenant_id}w{count}{conf.cluster_name}bkifn{mode}m0",
                "reviews": f"t{tenant_id}w{count}{conf.cluster_name}bkifn{mode}b0",
            }

            gateway_group = f"bkift{tenant_id}w{count}{mode}gg0"
            traffic_group = f"bkift{tenant_id}w{count}{mode}tg0"
            security_group = f"bkift{tenant_id}w{count}{mode}sg0"

            arguments = {
                "tenantName": f"tenant{tenant_id}",
                "orgName": org,
                "workspaceName": workspace_name,
                "clusterName": conf.cluster_name,
                "namespaces": namespaces,
                "gatewayGroupName": gateway_group,
                "trafficGroupName": traffic_group,
                "securityGroupName": security_group,
                "mode": current_mode.upper(),
                "securitySettingName": "bookinfo-security-setting",
                "reviewsHostFQDN": f"reviews.{namespaces['reviews']}.svc.cluster.local",
                "serviceRouteName": "bookinfo-serviceroute",
                "gatewayName": f"{namespaces['product']}-gateway",
                "hostname": f"{namespaces['product']}.tetrate.test.com",
                "gwSecretName": f"{namespaces['product']}-credential",
                "productHostFQDN": f"productpage.{namespaces['product']}.svc.cluster.local",
                "password": password,
                "servicerouteSAName": f"{namespaces['reviews']}-editor",
                "servicerouteEditorPodName": f"{namespaces['reviews']}-editorpod",
                "provider": provider,
                "tctlVersion": tctl_ver,
                "destinationruleName": "bookinfo-destinationrule",
                "destinationFQDN": f"productpage.{namespaces['product']}.svc.cluster.local",
                "virtualserviceName": "bookinfo-virtualservice",
                "ipType": "InternalIP"
                if conf.traffic_gen_ip == "internal"
                else "ExternalIP",
            }

            k8s_objects.generate_bookinfo(
                arguments, f"generated/k8s-objects/{key}/bookinfo.yaml"
            )

            gen_common_tsb_objects(arguments, key)

            k8s_objects.generate_bookinfo_namespaces(
                arguments, f"generated/k8s-objects/{key}/01namespaces.yaml"
            )

            if current_mode == "bridged":
                gen_bridge_specific_objects(
                    arguments,
                    key,
                )
            else:
                gen_direct_specific_objects(
                    arguments,
                    key,
                )

            k8s_objects.generate_ingress(
                arguments, f"generated/k8s-objects/{key}/ingress.yaml"
            )

            certs.create_private_key(namespaces["product"])
            certs.create_cert(namespaces["product"])
            certs.create_secret(
                namespaces["product"], f"generated/k8s-objects/{key}/secret.yaml"
            )

            arguments["secretName"] = certs.create_trafficgen_secret(
                namespaces["product"],
                f"generated/k8s-objects/{key}/{namespaces['product']}-secret.yaml",
            )

            k8s_objects.generate_trafficgen_role(
                arguments, f"generated/k8s-objects/{key}/role.yaml"
            )

            k8s_objects.generate_trafficgen(
                arguments, f"generated/k8s-objects/{key}/traffic-gen.yaml"
            )

            print("Bookinfo installed\n")
            i += 1
            count += 1
    return count

def main():
    parser = argparse.ArgumentParser(description="Spin up bookinfo instances")

    parser.add_argument(
        "--config",
        help="the yaml configuration for the bookinfo instances",
        required=True,
    )
    parser.add_argument(
        "--password",
        help="password for the admin user in the tsb instance",
        default="admin",
    )
    args = parser.parse_args()

    configs = config.read_config_yaml(args.config)

    if configs.provider not in ["aws", "others"]:
        print(
            "Possible values for provider is `aws` and `others` not", configs.provider
        )
        sys.exit(1)

    certs.create_root_cert()

    os.makedirs("generated/", exist_ok=True)
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
            f"generated/tenant{tenant_id}.yaml",
        )

    for appconfig in configs.app:
        install_bookinfo(
            appconfig,
            args.password,
            configs.org,
            configs.provider,
            configs.tctl_version,
        )

if __name__ == "__main__":
    main()

"""
tenant = tenant-<id>
workspace = <app>-t<tenant_id>-ws<id>
groups = <app>-t<tenant_id>-w<id>-<mode>-<type><id>
namespace = t<tenant_id>-w<workspace_id>-<cluster_name>-<mode>-<app>-<type>-n<id>
"""
