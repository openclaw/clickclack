#!/usr/bin/env bash
set -euo pipefail

tag=${1:-}
release_dir=${2:-}
if [[ ! "$tag" =~ ^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?)$ || ! -d "$release_dir" ]]; then
  echo "usage: $0 vMAJOR.MINOR.PATCH release-directory" >&2
  exit 2
fi
version=${tag#v}
release_dir=$(cd "$release_dir" && pwd)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/clickclack-server-verify.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT
requirement='identifier "chat.clickclack.server" and anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] exists and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = "FWJYW4S8P8"'
(
  cd "$release_dir"
  shasum -a 256 --check "ClickClack-$version-mac-server-SHA256SUMS.txt"
)
for arch in amd64 aarch64; do
  archive="$release_dir/clickclack_${version}_darwin_${arch}.tar.gz"
  mkdir "$work_dir/$arch"
  python3 - "$archive" "$work_dir/$arch" <<'PY'
import sys, tarfile
with tarfile.open(sys.argv[1]) as archive:
    archive.extractall(sys.argv[2], filter="data")
PY
  binary="$work_dir/$arch/clickclack"
  test -f "$binary" && test ! -L "$binary"
  expected_arch=x86_64
  [[ "$arch" != aarch64 ]] || expected_arch=arm64
  test "$(lipo -archs "$binary")" = "$expected_arch"
  codesign --verify --strict -R="$requirement" --verbose=2 "$binary"
  codesign --verify --strict --check-notarization -R=notarized --verbose=2 "$binary"
  codesign -dvvv "$binary" 2>&1 | grep -Eq '^CodeDirectory .*flags=.*\([^)]*runtime[^)]*\)'
  minos=$(otool -l "$binary" | awk '/LC_BUILD_VERSION/{build=1;next} build && /minos/{print $2;exit}')
  test "$minos" = 13.0
  if [[ "$(uname -m)" = "$expected_arch" ]]; then
    "$binary" version | grep -F "$version"
  fi
done
echo "verified signed and notarized ClickClack $version macOS server archives (macOS 13 minimum)"
