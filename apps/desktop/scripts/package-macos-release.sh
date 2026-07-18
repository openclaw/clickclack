#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repo=$(cd "$root/../.." && pwd)
tag=${1:-}
identity_qualifier='OpenClaw Foundation (FWJYW4S8P8)'

if [[ ! "$tag" =~ ^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?)$ ]]; then
  echo "usage: $0 vMAJOR.MINOR.PATCH" >&2
  exit 2
fi
if [[ "$(uname -s)" != Darwin ]]; then
  echo "official macOS packaging must run on macOS" >&2
  exit 1
fi
if [[ -z "${NOTARYTOOL_KEYCHAIN_PROFILE:-}" ]]; then
  echo "official macOS packaging requires NOTARYTOOL_KEYCHAIN_PROFILE" >&2
  exit 1
fi

head_commit=$(git -C "$repo" rev-parse HEAD)
tag_commit=$(git -C "$repo" rev-parse "refs/tags/$tag^{commit}" 2>/dev/null) || {
  echo "release tag does not exist locally: $tag" >&2
  exit 1
}
if [[ "$head_commit" != "$tag_commit" ]]; then
  echo "HEAD does not match release tag $tag" >&2
  exit 1
fi
if [[ -n "$(git -C "$repo" status --porcelain --untracked-files=normal)" ]]; then
  echo "release checkout is not clean" >&2
  exit 1
fi
git -C "$repo" tag -v "$tag" >/dev/null 2>&1 || {
  echo "release tag is not signed by a trusted git signing key: $tag" >&2
  exit 1
}

version=${tag#v}
cd "$root"
pnpm build
rm -rf release
CLICKCLACK_OFFICIAL_MACOS_RELEASE=1 \
  CSC_IDENTITY_AUTO_DISCOVERY=true \
  CSC_NAME="$identity_qualifier" \
  pnpm exec electron-builder \
    --mac dmg zip --x64 --arm64 \
    --config.mac.identity="$identity_qualifier" \
    --config.extraMetadata.version="$version" \
    --publish never

"$root/scripts/verify-macos-release.sh" "$tag" "$root/release"
node "$root/scripts/release-artifacts.mjs" mac "$version" "$root/release"
