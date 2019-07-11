.DEFAULT_GOAL := help

.PHONY: docker-login
docker-login: ## Login to Dockerhub
	./build/docker/docker-login.sh

.PHONY: build-image
build-image: docker-login ## Build Toolbox Docker image
	./build/docker/build.sh

.PHONY: publish-image
publish-image: docker-login ## Publish Toolbox Docker image
	./build/docker/push.sh

.PHONY: aks-login
aks-login: ## Login to Azure Kubernetes Service
	./build/bin/aks-login.sh

.PHONY: deploy
deploy: ## Deploy to cloud (dev environment)
	./build/bin/deploy.sh

.PHONY: delete-deployment
delete-deployment: ## Deploy to cloud (dev environment)
	./build/bin/delete-deployment.sh

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' && echo "NOTE: You can find Makefile goals implementation stored in \"./build\" directory"
