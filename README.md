# buhtig-s8k

Reflect some Github events in Kubernetes cluster

This app deletes namespace and corresponding Helm release for development branch on Github in case this branch was deleted (e.g. merged and deleted as obsolete).

## What's about the name?

This is `k8s-github` reversed.

> There are two hard problems in programming - cache invalidation and naming things.
> - Somebody

GitOps practice implies that state of a cluster reflects source code. While `Git -> K8s` (Git pushes changes to K8s) flow is easier to grasp, in the case of this service we have `K8s -> Git` flow, where service analyzes its own resources (namespaces) and then pulls information from Git instead. This is why the name is reversed.

## Why is it needed

When a development branch is created in application's Github repository corresponding deployment is created in Azure cluster. When development completes a pull request is created, and when it is merged the branch becomes obsolete (because it's already merged to main branch) and should be deleted either via Github UI or `git push --delete origin DEV_BRANCH_NAME`. Deployment becomes obsolete and should be deleted in order to release cluster resources.

## How it works

It runs as a single service in its own namespace and handles this task in Kubernetes-ish way. How it works:
- if certain deployment (read: namespace) is a development branch which is target for this cleanup logic, then we just label this namespace with this `label`: `opuscapita.com/buhtig-s8k: true` and annotate with `annotations`:
  - `opuscapita.com/github-source-url: https://github.com/OpusCapita/repository/tree/my-dev-branch`
  - `opuscapita.com/helm-release: dev-repository-my-dev-branch`
- our service runs every minute: find namespace with aforementioned label, query `github-source-url` and in case it returns 404 delete provided Helm release and delete namespace itself
