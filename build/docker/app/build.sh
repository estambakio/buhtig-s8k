#!/usr/bin/env bash

set -eo pipefail

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

docker_repository=$( $script_dir/../bin/get-docker-repository.sh )

docker_tag=$( $script_dir/../bin/get-docker-tag.sh )

project_root_dir=$( cd "$script_dir" && git rev-parse --show-toplevel )

docker build \
  --build-arg CREATED=$( date -u +"%Y-%m-%dT%H:%M:%SZ" ) \
  --build-arg SOURCE="$( git remote get-url origin )" \
  --build-arg REVISION="$( git rev-parse --verify HEAD )" \
  -t "$docker_repository":"$docker_tag" \
  -f ${script_dir}/Dockerfile \
  ${project_root_dir}
