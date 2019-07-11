#!/bin/sh

set -eo pipefail

echo "[INFO] Install Hashicorp Vault"

wget -O /vault.zip https://releases.hashicorp.com/vault/1.1.0/vault_1.1.0_linux_amd64.zip; unzip /vault.zip -d /usr/bin/

echo "[INFO] Update DockerHub credentials for current CircleCI project"

dockerhub_user=$( vault kv get -field=value minsk-core-kv/machineuser/dockerhub/username )
dockerhub_password=$( vault kv get -field=value minsk-core-kv/machineuser/dockerhub/password )
circleci_token=$( vault kv get -field=value minsk-core-kv/machineuser/circleci/token )

api_url="https://circleci.com/api/v1.1/project/github/${CIRCLE_PROJECT_USERNAME}/${CIRCLE_PROJECT_REPONAME}/envvar?circle-token=${circleci_token}"

curl --fail -X POST -H "Content-Type:application/json" -d '{"name":"'"DOCKERHUB_USER"'","value":"'"$dockerhub_user"'"}' "$api_url"
curl --fail -X POST -H "Content-Type:application/json" -d '{"name":"'"DOCKERHUB_PASSWORD"'","value":"'"$dockerhub_password"'"}' "$api_url"

echo "[INFO] DOCKERHUB_USER and DOCKERHUB_PASSWORD CircleCI environment variables successfully updated for current project"
