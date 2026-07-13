#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export CLICKCLACK_BUILD_FLAVOR="${CLICKCLACK_BUILD_FLAVOR:-fakeco}"

# Diagnostics and teardown still need Compose interpolation metadata, but they
# must remain usable while the checkout is dirty.
export CLICKCLACK_REQUIRE_CLEAN_TREE=1
for argument in "$@"; do
  case "$argument" in
    ps|exec|down|logs|stop|start|restart|kill|top|events|images|config|ls|version|wait)
      export CLICKCLACK_REQUIRE_CLEAN_TREE=0
      break
      ;;
    build|up|run|create)
      break
      ;;
  esac
done
source "$script_dir/../../scripts/docker-build-metadata.sh"

exec docker compose \
  --project-directory "$script_dir" \
  --file "$script_dir/compose.yaml" \
  "$@"
