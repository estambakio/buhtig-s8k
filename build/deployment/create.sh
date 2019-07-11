#!/usr/bin/env bash

set -eo pipefail

# make sure 'ytt' is installed
command -v ytt >/dev/null 2>&1 || {
  echo >&2 "$(basename $0) requires 'ytt' but it's not installed. Aborting."
  exit 1
}

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

branch="$( git rev-parse --abbrev-ref HEAD )"
safe_branch=$( $script_dir/../bin/generate-k8s-resource-name.sh $branch )

github_project="$( $script_dir/../bin/github-project.sh )"
github_reponame=$( echo $github_project | cut -d "/" -f 2 | tr '[:upper:]' '[:lower:]' )

namespace=$( $script_dir/../bin/generate-k8s-resource-name.sh "dev-${github_reponame}-${safe_branch}" )

docker_repository=$( $script_dir/../docker/bin/get-docker-repository.sh )
docker_tag=$( $script_dir/../docker/bin/get-docker-tag.sh )

github_token=$( vault kv get -field=value minsk-core-kv/machineuser/github/token )

# deploy

. $script_dir/../bin/assert-vars.sh namespace docker_repository docker_tag github_token

ytt \
  -v namespace="${namespace}" \
  -v image="${docker_repository}:${docker_tag}" \
  -v github.token="${github_token}" \
  -f ${script_dir} | \
  kubectl apply -f -
