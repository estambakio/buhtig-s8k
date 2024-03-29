#!/usr/bin/env bash

set -eo pipefail

github_project_url=$( git remote get-url origin )

if [[ $github_project_url == git@github.com:* ]] ; then
  url_without_prefix=${github_project_url#"git@github.com:"}
elif [[ $github_project_url == https://github.com/* ]] ; then
  url_without_prefix=${github_project_url#"https://github.com/"}
else
  echo "'$github_project_url' is not recognised as GitHub repo URL"
  exit 1
fi

if [[ $url_without_prefix == *.git ]] ; then
  github_project="${url_without_prefix%".git"}"
else
  github_project="$url_without_prefix"
fi

echo "$github_project"
