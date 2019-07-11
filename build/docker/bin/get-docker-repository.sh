#!/usr/bin/env bash

set -eo pipefail

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

docker_repository=$( echo "$( . $script_dir/../../bin/github-project.sh )" | tr '[:upper:]' '[:lower:]' )

echo $docker_repository
