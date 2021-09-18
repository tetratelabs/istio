#!/usr/bin/env bash

set -e
set -u

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd -P )"

INTEG_TEST_SUITE="${@:$#}"   # last argument
INTEG_TEST_ARGS="${*%${!#}}" # all arguments but last

# swap entrypoint of the Istio 'build-tools' image to upgrade KinD on container start up
export DOCKER_RUN_OPTIONS="${DOCKER_RUN_OPTIONS:-} \
  -v ${SCRIPT_DIR}/update_kind.sh:/usr/local/bin/update_kind.sh \
  --entrypoint /usr/local/bin/update_kind.sh"

# swap MAKE_DOCKER variable of the Makefile to run all commands inside the container
make MAKE_DOCKER="./common/scripts/run.sh prow/integ-suite-kind.sh ${INTEG_TEST_ARGS}" "${INTEG_TEST_SUITE}"
