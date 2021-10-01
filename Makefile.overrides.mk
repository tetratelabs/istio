# Copyright 2019 Istio Authors
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

.DEFAULT_GOAL := default

# This repository has been enabled for BUILD_WITH_CONTAINER=1. Some
# test cases fail within Docker, and Mac + Docker isn't quite perfect.
# For more information see: https://github.com/istio/istio/pull/19322/

BUILD_WITH_CONTAINER ?= 1
CONTAINER_OPTIONS = --mount type=bind,source=/tmp,destination=/tmp --net=host

ifeq ($(PROXY_REPO_SHA),)
  export PROXY_REPO_SHA:=$(shell grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
endif
# We override these variables to build envoy with our modsecurity filter, and push it to our private storage.
export ISTIO_ENVOY_BASE_URL ?= gs://tetrate-istio-build/proxy
export ISTIO_ENVOY_RELEASE_URL ?= $(ISTIO_ENVOY_BASE_URL)/envoy-alpha-modsecurity-$(PROXY_REPO_SHA).tar.gz

ifeq ($(BUILD_WITH_CONTAINER),1)
# create phony targets for the top-level items in the repo
PHONYS := $(shell ls | grep -v Makefile)
.PHONY: $(PHONYS)
$(PHONYS):
	@$(MAKE_DOCKER) $@
endif

# istioctl-install builds then installs istioctl into $GOPATH/BIN
# Used for debugging istioctl during dev work
.PHONY: istioctl-install
istioctl-install: istioctl-install-container
	cp out/$(TARGET_OS)_$(TARGET_ARCH)/istioctl ${GOPATH}/bin

# Update to focal/debian10
# see https://github.com/istio/istio/pull/33231
BASE_VERSION ?= 1.11-dev.8
