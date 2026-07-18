#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tag=${1:-}
release_dir=${2:-"$root/release"}

if [[ ! "$tag" =~ ^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?)$ ]]; then
  echo "usage: $0 vMAJOR.MINOR.PATCH [release-directory]" >&2
  exit 2
fi
if [[ "$(uname -s)" != Darwin ]]; then
  echo "macOS release verification must run on macOS" >&2
  exit 1
fi

version=${tag#v}
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/clickclack-macos-verify.XXXXXX")
mounted_volume=""
cleanup() {
  if [[ -n "$mounted_volume" ]]; then
    hdiutil detach "$mounted_volume" -quiet || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

verify_app() {
  local app=$1 expected_arch=$2 actual_archs
  CLICKCLACK_EXPECTED_VERSION="$version" \
    "$root/scripts/verify-macos-app.sh" --require-notarized "$app"
  actual_archs=$(lipo -archs "$app/Contents/MacOS/ClickClack")
  if [[ "$actual_archs" != "$expected_arch" ]]; then
    echo "unexpected ClickClack architecture: $actual_archs" >&2
    exit 1
  fi
}

for arch in arm64 x64; do
  if [[ "$arch" == arm64 ]]; then
    expected_arch=arm64
  else
    expected_arch=x86_64
  fi
  zip="$release_dir/ClickClack-$version-mac-$arch.zip"
  dmg="$release_dir/ClickClack-$version-mac-$arch.dmg"
  if [[ ! -f "$zip" || ! -f "$dmg" ]]; then
    echo "missing macOS release artifacts for $arch in $release_dir" >&2
    exit 1
  fi

  zip_dir="$work_dir/zip-$arch"
  mkdir -p "$zip_dir"
  ditto -x -k "$zip" "$zip_dir"
  verify_app "$zip_dir/ClickClack.app" "$expected_arch"

  mount_dir="$work_dir/dmg-$arch"
  mkdir -p "$mount_dir"
  hdiutil attach "$dmg" -nobrowse -readonly -mountpoint "$mount_dir" -quiet
  mounted_volume="$mount_dir"
  verify_app "$mount_dir/ClickClack.app" "$expected_arch"
  hdiutil detach "$mount_dir" -quiet
  mounted_volume=""
done

echo "verified notarized ClickClack $version macOS ZIP and DMG artifacts"
