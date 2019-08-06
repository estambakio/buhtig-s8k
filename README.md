[![CircleCI](https://circleci.com/gh/OpusCapita/buhtig-s8k.svg?style=shield)](https://circleci.com/gh/OpusCapita/buhtig-s8k)
[![Go Report Card](https://goreportcard.com/badge/github.com/OpusCapita/buhtig-s8k)](https://goreportcard.com/report/github.com/OpusCapita/buhtig-s8k)
[![license](https://img.shields.io/github/license/OpusCapita/buhtig-s8k.svg?style=flat-square)](LICENSE)

# buhtig-s8k

Reflect some Github events in Kubernetes cluster

This app deletes namespace and corresponding Helm release for development branch on Github in case this branch was deleted (e.g. merged and deleted as obsolete).

## Why is it needed

When a development branch is created in application's Github repository corresponding deployment is created in Azure cluster. When development completes a pull request is created, and when it is merged the branch becomes obsolete (because it's already merged to main branch) and should be deleted either via Github UI or `git push --delete origin DEV_BRANCH_NAME`. Deployment becomes obsolete and should be deleted in order to release cluster resources.

## How it works

It runs as a single service in its own namespace and handles this task in Kubernetes-ish way. Service runs every minute: if certain namespace is marked as a development branch which is target for this cleanup logic, then service queries Github API in order to determine status of branch and in case it returns 404 deletes corresponding Helm release and deletes the namespace itself.

## Usage

### How to track namespace

App uses `labels` and `annotations` to define relevant namespaces.

Namespace should have:
- `label` with name `opuscapita.com/buhtig-s8k` and value `"true"`
- `annotation` with name `opuscapita.com/github-source-url` and value like `https://github.com/OWNER/REPOSITORY/tree/BRANCH` - if this branch is deleted from Github then application will delete this namespace
- `annotation` with name `opuscapita.com/helm-release` and value equal to Helm release which should be deleted along with namespace

Example:

```
apiVersion: v1
kind: Namespace
metadata:
  name: dev-some-repo-issue-34
  labels:
    opuscapita.com/buhtig-s8k: "true"
  annotations:
    opuscapita.com/github-source-url: https://github.com/OpusCapita/some-repo/tree/issue-34
    opuscapita.com/helm-release: "dev-some-repo-issue-34"
```

If branch `issue-34` is deleted from repository `OpusCapita/some-repo` then application will:
- delete Helm release `dev-some-repo-issue-34`
  (in the same fashion as `helm delete --purge dev-some-repo-issue-34`)
- delete namespace `dev-some-repo-issue-34`

### Testing

`make test`

### Running

```
export GH_TOKEN=...

// running outside of Kubernetes cluster (e.g. for development)
APP_ENV=outside_cluster go run ./cmd
// OR (the same)
make run

// running inside cluster
go run ./cmd
```

### Building

`make build`

### Required environment

App requires the following environment variables in scope:
- `GH_TOKEN` - access token for authenticating requests to Github (e.g. personal access token)

### Additional configuration

Also the following environment can be specified:
- `TILLER_NAMESPACE` - default is "kube-system", specify your own if Tiller is installed in a different namespace

## What's about the name?

This is `k8s-github` reversed.

> There are two hard problems in programming - cache invalidation and naming things.
> - Somebody

GitOps practice implies that state of a cluster reflects source code. While `Git -> K8s` (Git pushes changes to K8s) flow is easier to grasp, in the case of this service we have `K8s -> Git` flow, where service analyzes its own resources (namespaces) and then pulls information from Git instead. This is why the name is reversed.


