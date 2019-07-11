#!/usr/bin/env bash

set -eo pipefail

if [ $# -eq "0" ]; then
  printf "Check if veriables are set in scope\n"
  printf "Usage: check-env.sh GH_NAME GH_PASS\n"
  exit 1
fi

for v in $@; do
  if [[ -z "${!v}" ]]; then
    printf "Error: $v is empty\n"
    exit 1
  fi
done
