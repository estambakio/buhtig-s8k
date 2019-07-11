#!/usr/bin/env bash

# $1 - text to convert into DNS-1123 compatible format https://tools.ietf.org/html/rfc1123

# Usage:
# generate-k8s-resource-name.sh name-to-convert

# Example:
# generate-k8s-resource-name.sh my.name/bob -> my-name-bob

echo $1 | sed -E s/[^a-zA-Z0-9]+/-/g | sed -E s/^-+\|-+$//g | tr A-Z a-z | cut -c1-53
