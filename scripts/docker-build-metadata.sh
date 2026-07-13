#!/usr/bin/env bash

set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
build_flavor="${CLICKCLACK_BUILD_FLAVOR:-local}"

if [[ ! "$build_flavor" =~ ^[0-9A-Za-z][0-9A-Za-z.-]*$ ]]; then
  echo "invalid CLICKCLACK_BUILD_FLAVOR: $build_flavor" >&2
  return 1 2>/dev/null || exit 1
fi

if [[ -n "$(git -C "$repository_root" status --porcelain --untracked-files=normal)" ]]; then
  echo "Docker builds require a clean Git worktree so embedded metadata matches the image." >&2
  return 1 2>/dev/null || exit 1
fi

CLICKCLACK_COMMIT="$(git -C "$repository_root" rev-parse --verify HEAD)"
if [[ ! "$CLICKCLACK_COMMIT" =~ ^[0-9a-f]{40}$ ]]; then
  echo "invalid Git commit: $CLICKCLACK_COMMIT" >&2
  return 1 2>/dev/null || exit 1
fi

CLICKCLACK_BUILD_DATE="$(git -C "$repository_root" show -s --format=%cI "$CLICKCLACK_COMMIT")"
if [[ ! "$CLICKCLACK_BUILD_DATE" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T ]]; then
  echo "invalid Git commit date: $CLICKCLACK_BUILD_DATE" >&2
  return 1 2>/dev/null || exit 1
fi

CLICKCLACK_VERSION="${CLICKCLACK_VERSION:-0.0.0-${build_flavor}.${CLICKCLACK_COMMIT:0:12}}"
if [[ ! "$CLICKCLACK_VERSION" =~ ^[0-9A-Za-z][0-9A-Za-z.+-]*$ ]]; then
  echo "invalid CLICKCLACK_VERSION: $CLICKCLACK_VERSION" >&2
  return 1 2>/dev/null || exit 1
fi

CLICKCLACK_WEB_VERSION="${CLICKCLACK_WEB_VERSION:-$CLICKCLACK_COMMIT}"
export CLICKCLACK_VERSION CLICKCLACK_COMMIT CLICKCLACK_BUILD_DATE CLICKCLACK_WEB_VERSION

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'CLICKCLACK_VERSION=%s\n' "$CLICKCLACK_VERSION"
  printf 'CLICKCLACK_COMMIT=%s\n' "$CLICKCLACK_COMMIT"
  printf 'CLICKCLACK_BUILD_DATE=%s\n' "$CLICKCLACK_BUILD_DATE"
  printf 'CLICKCLACK_WEB_VERSION=%s\n' "$CLICKCLACK_WEB_VERSION"
fi
