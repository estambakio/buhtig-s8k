#!/usr/bin/env bash

set -eo pipefail

echo "[INFO] Trying to authenticate to Docker registry"

dockerhub_user=$( vault kv get -field=value minsk-core-kv/machineuser/dockerhub/username )
dockerhub_password=$( vault kv get -field=value minsk-core-kv/machineuser/dockerhub/password )

echo "[INFO] Trying to authenticate to Docker registry as user: $dockerhub_user"

echo "$dockerhub_password" | docker login -u "$dockerhub_user"  --password-stdin
