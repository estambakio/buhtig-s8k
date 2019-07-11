#!/usr/bin/env bash

set -eo pipefail

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

branch="$( git rev-parse --abbrev-ref HEAD )"
safe_branch=$( $script_dir/../bin/generate-k8s-resource-name.sh $branch )

github_project="$( $script_dir/../bin/github-project.sh )"
github_reponame=$( echo $github_project | cut -d "/" -f 2 | tr '[:upper:]' '[:lower:]' )

namespace=$( $script_dir/../bin/generate-k8s-resource-name.sh "dev-${github_reponame}-${safe_branch}" )

# delete deployment

kubectl delete ns "${namespace}"
