#!/usr/bin/env bash

set -eo pipefail

script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

kubectl apply -f ${script_dir}/namespace.yaml

namespace="dev-egor-test"

helm init --client-only

helm upgrade \
  --install \
  --force \
  --namespace ${namespace} \
  ${namespace} \
  ${script_dir}/chart
