## Introduction

Google's BoringCrypto [module][1] is used for [FIPS-compliant Istio builds][2]. BoringCrypto is a core module of the
BoringSSL library and has been tested by CMVP to be [FIPS validated][3]. Both the Istio control plane and data plane
are built with these modules. The quickest way to get started with FIPS Istio is to use the
[Tetrate Istio Distribution][4].

## FIPS Build Process and Verification

### Istio Control Plane FIPS Build Process

This [doc](https://gokulchandrapr.medium.com/go-crypto-and-kubernetes-fips-140-2-fedramp-compliance-66d852ccccd2)
provides a good introduction on what's necessary to build a FIPS compliant go binaries by using boring crypto.

Roughy speaking, we need to:

1. Use boringcrypto version Golang toolset. In TID, we set up golang binaries differently, see [setup_boring_go.sh](https://github.com/istio/istio/blob/f7d03be560753dc71d4c764dceb06f961c4fcdbd/tetrateci/setup_boring_go.sh#L25).
1. Configure `CGO_ENABLED=1` when invoking `go build`. Istio uses a wrapper `gobuild.sh` script to build all Go binaries.
In TID, we ensure the `CGO_ENABLED=1` is patched to this script. For example, in [tetratefips-release-1.9](https://github.com/tetratelabs/istio/blob/tetratefips-release-1.9/common/scripts/gobuild.sh#L53).
  TODO(psbrar99): provide link on other releases when we updated from 1.10 and beyond.
1. Optionally, some tutorial may suggest to verify the FIPS compliant build via `go tool nm ./out/linux_amd64/pilot-discovery | grep 'boringcrypto.*'`.
  For now, when you run this against TID go binaries, you may not see the symbols, because currently Istio(TID as well) strips off
  the symbols by providing [`-ldflags  '-extldflags -static -s -w'`](https://github.com/tetratelabs/istio/blob/tetratefips-release-1.9/Makefile.core.mk#L270).
  TODO(incfly): consider whether to retain the symbols this as part of the final build.

### Istio Data Plane Build Process

TODO(incfly): describe how envoy proxy is built with FIPs version.

### Verify Golang Version Used for Build

The easiest way to verify the Go version is with Docker. First, create the containers from the [CloudSmith][5] images.
```shell
HUB=containers.istio.tetratelabs.com
TAG=1.11.4-tetratefips-v0
PILOT_CONTAINER_ID=$(docker create $HUB/pilot:$TAG)
PROXY_CONTAINER_ID=$(docker create $HUB/proxyv2:$TAG)
OPERATOR_CONTAINER_ID=$(docker create $HUB/operator:$TAG)
ISTIOCTL_CONTAINER_ID=$(docker create $HUB/istioctl:$TAG)
CNI_CONTAINER_ID=$(docker create $HUB/install-cni:$TAG)
```

Copy the binaries from the containers to your local machine.
```shell
docker cp $PILOT_CONTAINER_ID:/usr/local/bin/pilot-discovery pilot-discovery
docker cp $PROXY_CONTAINER_ID:/usr/local/bin/pilot-agent pilot-agent
docker cp $PROXY_CONTAINER_ID:/usr/local/bin/envoy envoy
docker cp $OPERATOR_CONTAINER_ID:/usr/local/bin/operator operator
docker cp $ISTIOCTL_CONTAINER_ID:/usr/local/bin/istioctl istioctl
docker cp $CNI_CONTAINER_ID:/usr/local/bin/install-cni install-cni
```

Verify the Go version used by the binaries.
```shell
go version pilot-discovery | cut -f2 -d" "
go version pilot-agent | cut -f2 -d" "
go version operator | cut -f2 -d" "
go version istioctl | cut -f2 -d" "
go version install-cni | cut -f2 -d" "
```
The Go version should include `b` to indicate BoringSSL, `go1.16.9b7` for example.

Verify Envoy is using BoringSSL FIPS:
```shell
envoy --version | cut -f4 -d" "
```

The version should include `BoringSSL-FIPS`, for example:
```shell
ed148b62dfb0dc79adc8c8573ced4806883389c0/1.19.2-dev/Modified/RELEASE/BoringSSL-FIPS
```

[1]: https://go.googlesource.com/go/+/dev.boringcrypto/README.boringcrypto.md
[2]: https://go-boringcrypto.storage.googleapis.com
[3]: https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/3678
[4]: https://istio.tetratelabs.io/
[5]: https://cloudsmith.io/~tetrate/repos/getistio-containers/packages/
