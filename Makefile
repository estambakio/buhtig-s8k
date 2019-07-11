.DEFAULT_GOAL := help

.PHONY: docker-login
docker-login: ## Login to Dockerhub
	./build/docker/bin/docker-login.sh

.PHONY: build-ci-image
build-ci-image: ## Build CI Docker image
	./build/docker/ci/build.sh

.PHONY: publish-ci-image
publish-ci-image: docker-login ## Publish CI Docker image
	./build/docker/ci/push.sh

.PHONY: compile
compile: ## Compile Go binary
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./dist/app ./cmd

.PHONY: build-app-image
build-app-image: ## Build App Docker image
	./build/docker/app/build.sh

.PHONY: publish-app-image
publish-app-image: docker-login ## Publish App Docker image
	./build/docker/app/push.sh

.PHONY: aks-login
aks-login: ## Login to Azure Kubernetes Service
	./build/bin/aks-login.sh

.PHONY: deploy
deploy: ## Deploy to Kubernetes cluster
	./build/deployment/create.sh

.PHONY: delete-deployment
delete-deployment: ## Delete deployment
	./build/deployment/delete.sh

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' && echo "NOTE: You can find Makefile goals implementation stored in \"./build\" directory"
