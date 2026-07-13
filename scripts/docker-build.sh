#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/docker-build-metadata.sh"

exec docker build \
  --build-arg "CLICKCLACK_VERSION=$CLICKCLACK_VERSION" \
  --build-arg "CLICKCLACK_COMMIT=$CLICKCLACK_COMMIT" \
  --build-arg "CLICKCLACK_BUILD_DATE=$CLICKCLACK_BUILD_DATE" \
  --build-arg "CLICKCLACK_WEB_VERSION=$CLICKCLACK_WEB_VERSION" \
  "$@"
