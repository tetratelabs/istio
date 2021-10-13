# Copyright (c) Tetrate, Inc 2021 All Rights Reserved.

# Override variables for Tetrate. Note that there are also Makefiles that have been directly modified.

BUILD_MODSECURITY ?= 0

# Populate the git version for istio/proxy (i.e. Envoy)
export ISTIO_ENVOY_BASE_URL ?= gs://tetrate-internal-istio-build/proxy
export TETRATE_MODSECURITYDEPS_RELEASE_URL ?= $(ISTIO_ENVOY_BASE_URL)/modsecurity-plugin-deps-$(ISTIO_ENVOY_VERSION).tar.gz

ifeq ($(BUILD_MODSECURITY),1)
  # Override them to replace envoy with our modsecurity version
  export TAG ?= modsecurity.$(shell git rev-parse --verify HEAD)
  export ISTIO_ENVOY_DEBUG_URL ?= $(ISTIO_ENVOY_BASE_URL)/envoy-debug-modsecurity-$(ISTIO_ENVOY_VERSION).tar.gz
  export ISTIO_ENVOY_RELEASE_URL ?= $(ISTIO_ENVOY_BASE_URL)/envoy-alpha-modsecurity-$(ISTIO_ENVOY_VERSION).tar.gz
  export ISTIO_ENVOY_LINUX_RELEASE_NAME ?= envoy-modsecurity-$(ISTIO_ENVOY_VERSION)
endif
