## Introduction

Google's BoringCrypto [module][1] is used for [FIPS-compliant Istio builds][2]. BoringCrypto is a core module of the
BoringSSL library and has been tested by CMVP to be [FIPS validated][3]. Both the Istio control plane and data plane
are built with these modules. The quickest way to get started with FIPS Istio is to use [our distribution][4].

## FIPS Verification

The easiest way to verify the Go version is with Docker. First, pull the container images from [CloudSmith][5]:
```shell
TAG=1.11.4-tetratefips-v0
$ docker pull containers.istio.tetratelabs.com/operator:$TAG
$ docker pull containers.istio.tetratelabs.com/pilot:$TAG
$ docker pull containers.istio.tetratelabs.com/proxyv2:$TAG
```

Get the operator, pilot, and proxyv2 image digests:
```shell
$ docker images --digests | grep $TAG
containers.istio.tetratelabs.com/operator 1.11.4-tetratefips-v0  sha256:62c37026ccf32b832a0c52e8ca15873c9b08212e893163bc72d4de3cbbe623be  ...
containers.istio.tetratelabs.com/proxyv2  1.11.4-tetratefips-v0  sha256:ba4b6bf458602af1706fd72c956ee2682afed1b87455f300ce6dfe79ba6eb35d  ...
containers.istio.tetratelabs.com/pilot    1.11.4-tetratefips-v0  sha256:f591c6c3059d036034d34fd9435b7923332a9f26582e3999960b6854407ec275  ...
...
```

Create the Dockerfile used to test the Istio operator build:
```shell
DIGEST=62c37026ccf32b832a0c52e8ca15873c9b08212e893163bc72d4de3cbbe623be
cat <<EOF >>Dockerfile.operator
FROM golang:1.16
COPY --from=containers.istio.tetratelabs.com/operator:$TAG@sha256:$DIGEST /usr/local/bin/operator /tmp/operator
EOF
```

Build the operator test image:
```shell
docker build -f Dockerfile.operator -t test-op .
```

Run `go version` from the container to verify the Go version used for the operator build.
```shell
docker run -it test-op go version /tmp/operator
```
__Note__: The Go version should include `b` to indicate BoringSSL, `go1.16.9b7` for example.

Create the Dockerfile used to test the Istio control-plane build:
```shell
DIGEST=f591c6c3059d036034d34fd9435b7923332a9f26582e3999960b6854407ec275
cat <<EOF >>Dockerfile.control-plane
FROM golang:1.16
COPY --from=containers.istio.tetratelabs.com/pilot:$TAG@sha256:$DIGEST /usr/local/bin/pilot-discovery /tmp/pilot-discovery
EOF
```

Build the control-plane test image:
```shell
docker build -f Dockerfile.control-plane -t test-cp .
```

Run `go version` from the container to verify the Go version used for the pilot build:
```shell
docker run -it test-cp go version /tmp/pilot-discovery
```

Create the Dockerfile used to test the Istio data-plane, i.e. proxyv2, build:
```shell
DIGEST=ba4b6bf458602af1706fd72c956ee2682afed1b87455f300ce6dfe79ba6eb35d
cat <<EOF >>Dockerfile.data-plane
FROM golang:1.16
COPY --from=containers.istio.tetratelabs.com/proxyv2:$TAG@sha256:$DIGEST /usr/local/bin/envoy /tmp/envoy
COPY --from=containers.istio.tetratelabs.com/proxyv2:$TAG@sha256:$DIGEST /usr/local/bin/pilot-agent /tmp/pilot-agent
EOF
```

Build the data-plane test image:
```shell
docker build -f Dockerfile.data-plane -t test-dp .
```

Run `go version` from the container image to verify the Go version used for the pilot-agent build.
```shell
docker run -it test-dp go version /tmp/pilot-agent
```

Verify Envoy is using BoringSSL FIPS:
```shell
docker run -it test-dp /tmp/envoy --version
```
__Note:__ The version should include `BoringSSL-FIPS`, for example:
```shell
/tmp/envoy  version: ed148b62dfb0dc79adc8c8573ced4806883389c0/1.19.2-dev/Modified/RELEASE/BoringSSL-FIPS
```

[1]: https://go.googlesource.com/go/+/dev.boringcrypto/README.boringcrypto.md
[2]: https://go-boringcrypto.storage.googleapis.com
[3]: https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/3678
[4]: https://istio.tetratelabs.io/
[5]: https://cloudsmith.io/~tetrate/repos/getistio-containers/packages/
