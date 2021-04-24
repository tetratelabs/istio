#!/usr/bin/env bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

SCRIPT_DIR=$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )

BASE_DIR="${SCRIPT_DIR}/.."

VM_FILES_DIR="${BASE_DIR}/etc"

set +e
docker rm --force istio-proxy

set -e
docker run -d --name istio-proxy --restart unless-stopped --network host -v "${VM_FILES_DIR}"/istio-ca.pem:/var/run/secrets/istio/root-cert.pem -v "${VM_FILES_DIR}"/istio-token:/var/run/secrets/tokens/istio-token --env-file "${VM_FILES_DIR}"/sidecar.env --add-host istiod.istio-system:1.2.3.4 --add-host istiod.istio-system.svc:1.2.3.4 --add-host istiod.istio-system.svc.cluster:1.2.3.4 --add-host istiod.istio-system.svc.cluster.local:1.2.3.4 --add-host zipkin.istio-system:1.2.3.4 --add-host zipkin.istio-system.svc:1.2.3.4 --add-host zipkin.istio-system.svc.cluster:1.2.3.4 --add-host zipkin.istio-system.svc.cluster.local:1.2.3.4 docker.io/istio/proxyv2:latest proxy sidecar --serviceCluster my-app.my-ns --concurrency 0 --proxyLogLevel info --proxyComponentLogLevel misc:info --trust-domain cluster.local

