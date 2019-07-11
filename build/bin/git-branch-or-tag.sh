#!/usr/bin/env bash

set -eo pipefail

git_revision=$( git log -1 --format=%H )

git describe --tags --exact-match "$git_revision" &>/dev/null && \
  echo "$( git describe --tags --exact-match $git_revision )" || \
  echo "$( git rev-parse --abbrev-ref HEAD )"
