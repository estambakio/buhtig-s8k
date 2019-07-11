#!/usr/bin/env bash

set -eo pipefail

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

docker_repository=$( $script_dir/../bin/get-docker-repository.sh )

docker_tag="ci"

docker build -t "$docker_repository":"$docker_tag" ${script_dir}
