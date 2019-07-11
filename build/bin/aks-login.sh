#!/usr/bin/env bash

set -euo pipefail

azure_user=$( vault kv get -field=value "minsk-core-kv/machineuser/azure/username" )
azure_password=$( vault kv get -field=value "minsk-core-kv/machineuser/azure/password" )
azure_subscription=$( vault kv get -field=value "minsk-core-kv/azure/subscriptions/dev-core/id" )
azure_kube_cluster_name=$( vault kv get -field=value "minsk-core-kv/kubernetes/azure/dev-core/name" )
azure_kube_cluster_resource_group=$( vault kv get -field=value "minsk-core-kv/kubernetes/azure/dev-core/resource-group" )

# login to Azure Kubernetes Service
az login -u "${azure_user}" -p "${azure_password}" &> /tmp/az-login.log
az account set -s "${azure_subscription}"
az aks get-credentials \
  -n "${azure_kube_cluster_name}" \
  -g "${azure_kube_cluster_resource_group}" \
  --overwrite-existing
