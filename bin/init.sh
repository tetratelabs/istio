#!/bin/bash

# Copyright 2018 Istio Authors
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

# Init script downloads or updates envoy and the go dependencies. Called from Makefile, which sets
# the needed environment variables.

set -o errexit
set -o nounset
set -o pipefail

if [[ "${ISTIO_ENVOY_LINUX_RELEASE_URL:-}" == "" ]]; then
  echo "Envoy variables no set. Make sure you run through the makefile (\`make init\`) rather than directly."
  exit 1
fi

# Download Envoy debug and release binaries for Linux x86_64. They will be included in the
# docker images created by Dockerfile.proxyv2.

# Gets the download command supported by the system (currently either curl or wget)
DOWNLOAD_COMMAND=""
function set_download_command () {
  # Try curl.
  if command -v curl > /dev/null; then
    if curl --version | grep Protocols  | grep https > /dev/null; then
      HTTP_CLIENT="curl -fLSs --retry 5 --retry-delay 1 --retry-connrefused"
      DOWNLOAD_COMMAND=curl_or_gsutil
      return
    fi
    echo curl does not support https, will try wget for downloading files.
  else
    echo curl is not installed, will try wget for downloading files.
  fi

  # Try wget.
  if command -v wget > /dev/null; then
    HTTP_CLIENT="wget -qO -"
    DOWNLOAD_COMMAND=wget_or_gsutil
    return
  fi
  echo wget is not installed.

  echo Error: curl is not installed or does not support https, wget is not installed. \
       Cannot download envoy. Please install wget or add support of https to curl.
  exit 1
}

# Wrap gsutil_or_http by curl_or_gsutil and wget_or_gsutil, because this init.sh assumes that
# DOWNLOAD_COMMAND starts with curl* or wget*, and chooses the right option according to whether
# the client is curl or wget.
function curl_or_gsutil () {
  if [[ "$HTTP_CLIENT" != curl* ]]; then
    return 1
  fi
  gsutil_or_http "$@"
}

function wget_or_gsutil () {
  if [[ "$HTTP_CLIENT" != wget* ]]; then
    return 1
  fi
  gsutil_or_http "$@"
}

# Gets the artifact by gsutil if the URL starts with gs://, otherwise delegate it to curl or wget.
function gsutil_or_http () {
  if [[ "$#" -lt 1 ]]; then
    echo "Error: no URL is given."
    echo "Usage: gsutil_or_http [--header val] URL [-O,-o file_name]"
    return 1
  fi

  local URL=""
  local OUT=""
  local ORIGINAL_ARGS=()
  while (( $# )); do
    case "$1" in
      --header | --retry-delay | --retry)
        # skip options that have value
        ORIGINAL_ARGS+=("$1" "$2")
        shift 2
        ;;
      -o|-O)
        OUT="$2"
        ORIGINAL_ARGS+=("$1" "$2")
        shift 2
        ;;
      -*)
        # skip options that have no value
        ORIGINAL_ARGS+=("$1")
        shift 1
        ;;
      *)
        URL="$1"
        ORIGINAL_ARGS+=("$1")
        shift 1
        ;;
    esac
  done

  if [[ ! "$URL" =~ ^gs:// ]]; then
    $HTTP_CLIENT "${ORIGINAL_ARGS[@]}"
    return $?
  fi

  if [[ -z "$OUT" ]]; then
    gsutil cat "$URL"
  else
    gsutil cp "$URL" "$OUT"
  fi

  return $?
}


# Downloads and extract an Envoy binary if the artifact doesn't already exist.
# Params:
#   $1: The URL of the Envoy tar.gz to be downloaded.
#   $2: The full path of the output binary.
#   $3: Non-versioned name to use
function download_envoy_if_necessary () {
  if [[ ! -f "$2" ]] ; then
    # Enter the output directory.
    mkdir -p "$(dirname "$2")"
    pushd "$(dirname "$2")"

    # Download and extract the binary to the output directory.
    echo "Downloading ${SIDECAR}: $1 to $2"
    time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" | tar xz

    # Copy the extracted binary to the output location
    cp usr/local/bin/"${SIDECAR}"* "$2"

    # Remove the extracted binary.
    rm -rf usr

    # Make a copy named just "envoy" in the same directory (overwrite if necessary).
    echo "Copying $2 to $(dirname "$2")/${3}"
    cp -f "$2" "$(dirname "$2")/${3}"
    popd
  fi
}

# Downloads and extract the runtime libraries ModSecurity requires if they don't already exist.
# Params:
#   $1: The URL of the libraries tar.gz to be downloaded.
#   $2: The full path of the output directory.
function download_modsecurity_deps_if_necessary () {
  out_path="$2/modsecurity_plugin_deps"
  if [[ ! -d "${out_path}" ]] ; then
    # Enter the output directory.
    pushd "$2"

    # Download and extract the binary to the output directory.
    echo "Downloading ModSecurity runtime dependencies: ${DOWNLOAD_COMMAND} $1 to ${out_path}"
    time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" | tar xz  # The extracted directory is "modsecurity_plugin_deps"
    popd
  fi
}

# Downloads WebAssembly based plugin if it doesn't already exist.
# Params:
#   $1: The URL of the WebAssembly file to be downloaded.
#   $2: The full path of the output file.
function download_wasm_if_necessary () {
  download_file_dir="$(dirname "$2")"
  download_file_name="$(basename "$1")"
  download_file_path="${download_file_dir}/${download_file_name}"
  if [[ ! -f "${download_file_path}" ]] ; then
    # Enter the output directory.
    mkdir -p "${download_file_dir}"
    pushd "${download_file_dir}"

    # Download the WebAssembly plugin files to the output directory.
    echo "Downloading WebAssembly file: $1 to ${download_file_path}"
    if [[ ${DOWNLOAD_COMMAND} == curl* ]]; then
      time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" -o "${download_file_name}"
    elif [[ ${DOWNLOAD_COMMAND} == wget* ]]; then
      time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" -O "${download_file_name}"
    fi

    # Copy the webassembly file to the output location
    cp "${download_file_path}" "$2"
    popd
  fi
}

# Downloads and extracts the OWASP Core Rule Set (CRS) to embed them to
# proxyv2 image for efficient distribution.
# Params:
#   $1: The URL of the  tar.gz to be downloaded.
#   $2: The expected sha1sum of the downloaded tar.gz.
#       Why sha1sum? Because the official site provides the verified sha1sum value as of 2021-06-21.
#   $3: The version string
#   $4: The full path of the output directory.
function download_crs_if_necessary () {
  if [[ ! -d "$4/$3" ]] ; then
    # Enter the output directory.
    mkdir -p "$4"/{"$3",tmp}
    pushd "$4/tmp"

    # Download and extract the binary to the output directory.
    echo "Downloading OWASP CRS: ${DOWNLOAD_COMMAND} $1 to $4"
    time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" -o "crs.tar.gz"
    if ! sha1sum --quiet -c <(echo "$2 crs.tar.gz"); then
      echo "Error: sha1sum of '$1' doesn't match. Expected: $2. Actual: $(sha1sum "crs.tar.gz")."
      exit 1
    fi
    tar xf crs.tar.gz

    # Copy the extracted binary to the output location
    cp ./*/rules/* ../"$3"/
    cp ./*/crs-setup.conf.example ../"$3"/crs-setup-default.conf

    # Remove the extracted binary.
    cd ..
    rm -rf tmp

    popd
  fi
}

mkdir -p "${ISTIO_OUT}"

# Set the value of DOWNLOAD_COMMAND (either curl or wget)
set_download_command

if [[ -n "${DEBUG_IMAGE:-}" ]]; then
  # Download and extract the Envoy linux debug binary.
  download_envoy_if_necessary "${ISTIO_ENVOY_LINUX_DEBUG_URL}" "$ISTIO_ENVOY_LINUX_DEBUG_PATH" "${SIDECAR}"
else
  echo "Skipping envoy debug. Set DEBUG_IMAGE to download."
fi

# Download and extract the Envoy linux release binary.
download_envoy_if_necessary "${ISTIO_ENVOY_LINUX_RELEASE_URL}" "$ISTIO_ENVOY_LINUX_RELEASE_PATH" "${SIDECAR}-modsecurity"
download_envoy_if_necessary "${ISTIO_ENVOY_CENTOS_RELEASE_URL}" "$ISTIO_ENVOY_CENTOS_LINUX_RELEASE_PATH" "${SIDECAR}-centos"

download_modsecurity_deps_if_necessary "${TETRATE_MODSECURITYDEPS_RELEASE_URL}" "${ISTIO_ENVOY_LINUX_RELEASE_DIR}"

if [[ "$GOOS_LOCAL" == "darwin" ]]; then
  # Download and extract the Envoy macOS release binary
  download_envoy_if_necessary "${ISTIO_ENVOY_MACOS_RELEASE_URL}" "$ISTIO_ENVOY_MACOS_RELEASE_PATH" "${SIDECAR}"
  ISTIO_ENVOY_NATIVE_PATH=${ISTIO_ENVOY_MACOS_RELEASE_PATH}
else
  ISTIO_ENVOY_NATIVE_PATH=${ISTIO_ENVOY_LINUX_RELEASE_PATH}
fi

# Download WebAssembly plugin files
WASM_RELEASE_DIR=${ISTIO_ENVOY_LINUX_RELEASE_DIR}
for plugin in stats metadata_exchange
do
  FILTER_WASM_URL="${ISTIO_ENVOY_BASE_URL}/${plugin}-${ISTIO_ENVOY_VERSION}.wasm"
  download_wasm_if_necessary "${FILTER_WASM_URL}" "${WASM_RELEASE_DIR}"/"${plugin//_/-}"-filter.wasm
  FILTER_WASM_URL="${ISTIO_ENVOY_BASE_URL}/${plugin}-${ISTIO_ENVOY_VERSION}.compiled.wasm"
  download_wasm_if_necessary "${FILTER_WASM_URL}" "${WASM_RELEASE_DIR}"/"${plugin//_/-}"-filter.compiled.wasm
done

# Download OWASP Core Rule Set files
CRS_RELEASE_DIR=${ISTIO_ENVOY_LINUX_RELEASE_DIR}/owasp-modsecurity-crs
CRS_VERSION="3.3.0"
CRS_URL="https://github.com/coreruleset/coreruleset/archive/refs/tags/v${CRS_VERSION}.tar.gz"
CRS_SHA1="1f4002b5cf941a9172b6250cea7e3465a85ef6ee"  # You can find the official value at https://coreruleset.org/installation/
download_crs_if_necessary "${CRS_URL}" "${CRS_SHA1}" "${CRS_VERSION}" "${CRS_RELEASE_DIR}"

# Copy native envoy binary to ISTIO_OUT
echo "Copying ${ISTIO_ENVOY_NATIVE_PATH} to ${ISTIO_OUT}/${SIDECAR}"
cp -f "${ISTIO_ENVOY_NATIVE_PATH}" "${ISTIO_OUT}/${SIDECAR}"

# Copy CentOS binary
echo "Copying ${ISTIO_ENVOY_CENTOS_LINUX_RELEASE_PATH} to ${ISTIO_OUT_LINUX}/${SIDECAR}-centos"
cp -f "${ISTIO_ENVOY_CENTOS_LINUX_RELEASE_PATH}" "${ISTIO_OUT_LINUX}/${SIDECAR}-centos"

# Copy the envoy binary to ISTIO_OUT_LINUX if the local OS is not Linux
if [[ "$GOOS_LOCAL" != "linux" ]]; then
   echo "Copying ${ISTIO_ENVOY_LINUX_RELEASE_PATH} to ${ISTIO_OUT_LINUX}/${SIDECAR}"
  cp -f "${ISTIO_ENVOY_LINUX_RELEASE_PATH}" "${ISTIO_OUT_LINUX}/${SIDECAR}"
fi
