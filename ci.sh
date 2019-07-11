#!/usr/bin/env bash

set -e

script_dir="$( cd "$( dirname "$BASH_SOURCE[0]" )" && pwd )"
project_root_dir=$( cd "${script_dir}" && git rev-parse --show-toplevel )

docker_repository=$( ${project_root_dir}/build/docker/get-docker-repository.sh )
docker_tag=$( ${project_root_dir}/build/docker/get-docker-tag.sh )
ci_docker_image="${docker_repository}:${docker_tag}"

echo "[INFO] Pulling newest preconfigured Docker image"

set +e
docker pull "${ci_docker_image}" || make build-image
set -e

echo "[INFO] Start preconfigured Docker container"

docker run \
  --rm \
  -it \
  --workdir=/application \
  -e VAULT_ADDR="${VAULT_ADDR}" \
  -e VAULT_TOKEN="${VAULT_TOKEN}" \
  -v "${project_root_dir}":/application:delegated \
  -v /var/run/docker.sock:/var/run/docker.sock \
  "${ci_docker_image}" bash -c "$*"
