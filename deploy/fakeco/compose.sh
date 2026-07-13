#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export CLICKCLACK_BUILD_FLAVOR="${CLICKCLACK_BUILD_FLAVOR:-fakeco}"
source "$script_dir/../../scripts/docker-build-metadata.sh"

exec docker compose \
  --project-directory "$script_dir" \
  --file "$script_dir/compose.yaml" \
  "$@"
