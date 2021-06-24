import os
import sys
from jinja2 import Template
import yaml
import argparse
from dataclasses import dataclass
from marshmallow_dataclass import class_schema, marshmallow
import certs
import tsb_objects, k8s_objects, common

@dataclass
class config:
    count: int
    org: str
    cluster: str
    mode: str

def read_config_yaml(filename):
    schema = class_schema(config)
    with open(filename) as file:
        yamlconfig = yaml.load(file, Loader=yaml.SafeLoader)
        return schema().load(yamlconfig)

script_path = os.path.dirname(os.path.realpath(__file__))

def save_file(fname, content):
    f = open(fname, "w")
    f.write(content)
    f.close()

def install_bookinfo(ns, key, folder):
    svc_domain = ".svc.cluster.local"
    details_env = "details-" + key + "." + ns + svc_domain
    reviews_env = "reviews-" + key + "." + ns + svc_domain
    ratings_env = "ratings-" + key + "." + ns + svc_domain

    t = open(script_path + "/templates/k8s-objects/bookinfo-single.yaml")
    template = Template(t.read())
    r = template.render(
        ns=ns,
        detailsHostName=details_env,
        reviewsHostName=reviews_env,
        ratingsHostName=ratings_env,
        id=key,
    )
    t.close()
    save_file(f"{folder}/k8s-objects/bookinfo-" + key + ".yaml", r)
    pass

def main():
    parser = argparse.ArgumentParser(
        description="Spin up bookinfo instances, all the flags are required and to be pre generated\n"
        + "Example:\n"
        + " pipenv run python bookinfo-single-gw.py --config bookinfo-single.example.yml\n",
        formatter_class=argparse.RawTextHelpFormatter,
    )

    parser.add_argument(
        "--config", help="pass the config for the install", required=True
    )

    parser.add_argument(
        "--folder",
        help="folder where the generated files would be stored",
        default=common.default_folder(),
    )
    args = parser.parse_args()
    folder = args.folder
    try:
        conf = read_config_yaml(args.config)
    except marshmallow.exceptions.ValidationError as e:
        print("Validation errors in the configuration file.")
        print(e)
        sys.exit(1)
    except Exception as e:
        print(e)
        print("Unable to read the config file.")
        sys.exit(1)

    tenant = "tenant0"
    workspace = "bkift0ws0"
    namespace = f"t0w0{conf.cluster}bkifn{'d' if conf.mode == 'direct' else 'b'}0f"
    gateway_group = f"bkifot0w0bgg0"
    traffic_group = f"bkift0w0btg0"
    security_group = f"bkift0w0bsg0"

    try:
        os.makedirs(f"{folder}/k8s-objects/", exist_ok=True)
        os.makedirs(f"{folder}/tsb-objects/", exist_ok=True)
    except Exception as e:
        print(e)
        print("Error while creating the folders for the generated scripts.")
        sys.exit(1)

    namespace_yaml = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"labels": {"istio-injection": "enabled"}, "name": namespace},
    }

    arguments = {
        "orgName": conf.org,
        "tenantName": tenant,
        "workspaceName": workspace,
        "namespaces": namespace,
        "clusterName": conf.cluster,
        "mode": conf.mode.upper(),
        "gatewayGroupName": gateway_group,
        "trafficGroupName": traffic_group,
        "securityGroupName": security_group,
        "securitySettingName": f"bookinfo-security-setting-{namespace}",
    }
    try:
        tsb_objects.generate_tenant(arguments, f"{folder}/tsb-objects/tenant.yaml")
        tsb_objects.generate_workspace(
            arguments, f"{folder}/tsb-objects/workspaces.yaml"
        )
        tsb_objects.generate_groups(arguments, f"{folder}/tsb-objects/groups.yaml")
        tsb_objects.generate_perm(arguments, f"{folder}/tsb-objects/perm.yaml")

        f = open(f"{folder}/k8s-objects/01namespace.yaml", "w")
        yaml.dump(namespace_yaml, f)
        f.close()

        k8s_objects.generate_ingress(arguments, f"{folder}/k8s-objects/ingress.yaml")
        try:
            certs.generate_wildcard_cert(folder)
            certs.create_wildcard_secret(
                namespace, f"{folder}/k8s-objects/secret.yaml", folder
            )
            certs.create_trafficgen_wildcard_secret(
                namespace, f"{folder}/k8s-objects/trafficgen-secret.yaml", folder
            )
        except Exception as e:
            print(e)
            print("Error while generating the certificates.")
            sys.exit(1)
        http_routes = []
        curl_calls = []

        for i in range(conf.count):
            install_bookinfo(namespace, str(i), folder)
            name = "productpage-" + str(i)
            hostname = name + ".tetrate.test.com"

            curl_calls.append(
                f"curl https://{hostname}/productpage --connect-to {hostname}:443:$IP:$PORT --cacert /etc/bookinfo/bookinfo-ca.crt &>/dev/null"
            )

            ordered_arguments = {
                "orgName": conf.org,
                "tenantName": tenant,
                "workspaceName": workspace,
                "trafficGroupName": traffic_group,
                "gatewayGroupName": gateway_group,
                "reviewsHostFQDN": f"reviews-{i}.{namespace}.svc.cluster.local",
                "serviceRouteName": f"bookinfo-serviceroute-{i}",
                "namespaces": namespace,
                "hostname": hostname,
                "virtualserviceName": f"bookinfo-virtualservice-{i}",
                "gatewayName": "tsb-gateway",
                "destinationFQDN": f"productpage-{i}.{namespace}.svc.cluster.local",
                "destinationruleName": f"bookinfo-destinationrule-{i}",
            }
            if conf.mode == "bridged":
                http_routes.append(name)
                tsb_objects.generate_bridged_serviceroute(
                    ordered_arguments, f"{folder}/tsb-objects/serviceroute{i}.yaml"
                )
            else:
                http_routes.append(name)

                # virtual service for product page
                tsb_objects.generate_direct_vs(
                    ordered_arguments, f"{folder}/tsb-objects/virtualservice-{i}.yaml"
                )

                # destination rules
                tsb_objects.generate_direct_dr(
                    ordered_arguments, f"{folder}/tsb-objects/destinationrule-{i}.yaml"
                )

                # reviews virtual service
                tsb_objects.generate_direct_reviews_vs(
                    ordered_arguments, f"{folder}/tsb-objects/reviews_vs-{i}.yaml"
                )

        if conf.mode == "bridged":
            t = open(script_path + "/templates/tsb-objects/bridged/gateway-single.yaml")
            template = Template(t.read())
            r = template.render(
                orgName=conf.org,
                tenantName=tenant,
                workspaceName=workspace,
                gatewayGroupName=gateway_group,
                gatewayName="tsb-gateway",
                ns=namespace,
                entries=http_routes,
                secretName="wildcard-credential",
            )
            t.close()
            save_file(f"{folder}/tsb-objects/gateway.yaml", r)

            tsb_objects.generate_bridged_security(
                arguments, f"{folder}/tsb-objects/security.yaml"
            )

        else:
            t = open(script_path + "/templates/tsb-objects/direct/gw-single.yaml")
            template = Template(t.read())
            r = template.render(
                orgName=conf.org,
                tenantName=tenant,
                workspaceName=workspace,
                gatewayGroupName=gateway_group,
                gatewayName="tsb-gateway",
                ns=namespace,
                entries=http_routes,
                gwSecretName="wildcard-credential",
            )
            t.close()
            save_file(f"{folder}/tsb-objects/gateway.yaml", r)

        k8s_objects.generate_trafficgen_role(
            arguments, f"{folder}/k8s-objects/role.yaml"
        )

        t = open(script_path + "/templates/k8s-objects/traffic-gen-httpbin.yaml")
        template = Template(t.read())
        r = template.render(
            ns=namespace,
            saName=f"{namespace}-trafficgen-sa",
            secretName=namespace + "-ca-cert",
            serviceName="tsb-gateway-" + namespace,
            content=curl_calls,
        )
        t.close()
        save_file(f"{folder}/k8s-objects/traffic-gen.yaml", r)

        pass
    except Exception as e:
        print(e)
        print("Error while generating the yamls.")
        sys.exit(1)

if __name__ == "__main__":
    main()
