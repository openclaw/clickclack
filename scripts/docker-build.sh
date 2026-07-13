#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/docker-build-metadata.sh"

reject_reserved_build_arg() {
  local value=$1
  local name="${value%%=*}"
  case "$name" in
    CLICKCLACK_VERSION|CLICKCLACK_COMMIT|CLICKCLACK_BUILD_DATE|CLICKCLACK_WEB_VERSION)
      printf 'Build argument %s is derived by scripts/docker-build.sh and cannot be overridden.\n' \
        "$name" >&2
      exit 64
      ;;
  esac
}

expect_build_arg=0
for argument in "$@"; do
  if [[ "$expect_build_arg" == "1" ]]; then
    reject_reserved_build_arg "$argument"
    expect_build_arg=0
    continue
  fi
  case "$argument" in
    --build-arg)
      expect_build_arg=1
      ;;
    --build-arg=*)
      reject_reserved_build_arg "${argument#--build-arg=}"
      ;;
  esac
done

exec docker build \
  --build-arg "CLICKCLACK_VERSION=$CLICKCLACK_VERSION" \
  --build-arg "CLICKCLACK_COMMIT=$CLICKCLACK_COMMIT" \
  --build-arg "CLICKCLACK_BUILD_DATE=$CLICKCLACK_BUILD_DATE" \
  --build-arg "CLICKCLACK_WEB_VERSION=$CLICKCLACK_WEB_VERSION" \
  "$@"
