Manual testing helper for development purposes.

Access to Kubernetes cluster is required beforehand.

- in `namespace.yaml` set `opuscapita.com/github-source-url` to point to non-existing branch on Github (any repo you want)
- run `./deploy.sh` locally to create a test namespace 'dev-egor-test' and Helm release in Kubernetes cluster
- run `APP_ENV=outside_cluster GH_USER=... GH_TOKEN=... go run .` from repository root. If URL in namespace points to non-existing branch than both nmamespace and Helm release gonna be terminated
