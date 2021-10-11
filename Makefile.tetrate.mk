# Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

# Populate the git version for istio/proxy (i.e. Envoy)
ifeq ($(PROXY_REPO_SHA),)
  export PROXY_REPO_SHA:=$(shell grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
endif

# We override these variables to build envoy with our modsecurity filter, and push it to our private storage.
export ISTIO_ENVOY_BASE_URL ?= gs://tetrate-internal-istio-build/proxy
export ISTIO_ENVOY_RELEASE_URL ?= $(ISTIO_ENVOY_BASE_URL)/envoy-alpha-modsecurity-$(PROXY_REPO_SHA).tar.gz
export TETRATE_MODSECURITYDEPS_RELEASE_URL ?= $(ISTIO_ENVOY_BASE_URL)/modsecurity-plugin-deps-$(PROXY_REPO_SHA).tar.gz
