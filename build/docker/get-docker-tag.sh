#!/usr/bin/env bash

set -eo pipefail

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

generate_docker_tag() {
  echo $1 | sed -E s/[^a-zA-Z0-9\.]+/-/g | sed -E s/^-+\|-+$//g | cut -c1-128
}

docker_tag=$( generate_docker_tag "$( . $script_dir/../bin/git-branch-or-tag.sh )" )

echo $docker_tag
