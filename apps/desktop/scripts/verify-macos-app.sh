#!/usr/bin/env bash
set -euo pipefail

expected_identity='Developer ID Application: OpenClaw Foundation (FWJYW4S8P8)'
expected_team='FWJYW4S8P8'
expected_bundle_id='chat.clickclack.desktop'
requirement="identifier \"$expected_bundle_id\" and anchor apple generic and certificate 1[field.1.2.840.113635.100.6.2.6] exists and certificate leaf[field.1.2.840.113635.100.6.1.13] exists and certificate leaf[subject.OU] = \"$expected_team\""
require_notarized=0

if [[ "${1:-}" == --require-notarized ]]; then
  require_notarized=1
  shift
fi

app=${1:-}
if [[ -z "$app" ]]; then
  echo "usage: $0 [--require-notarized] <ClickClack.app>" >&2
  exit 2
fi
if [[ ! -d "$app" ]]; then
  echo "ClickClack app bundle not found: $app" >&2
  exit 1
fi

plist="$app/Contents/Info.plist"
main_executable="$app/Contents/MacOS/ClickClack"
if [[ ! -f "$plist" || ! -f "$main_executable" ]]; then
  echo "invalid ClickClack app layout: $app" >&2
  exit 1
fi

bundle_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist")
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$plist")
if [[ "$bundle_id" != "$expected_bundle_id" ]]; then
  echo "unexpected bundle identifier: $bundle_id" >&2
  exit 1
fi
if [[ -n "${CLICKCLACK_EXPECTED_VERSION:-}" && "$version" != "$CLICKCLACK_EXPECTED_VERSION" ]]; then
  echo "unexpected bundle version: $version" >&2
  exit 1
fi

codesign --verify --strict --deep --verbose=2 "$app"
codesign --verify --strict -R="$requirement" --verbose=2 "$app"

verify_signature() {
  local signed_path=$1 signature
  signature=$(codesign -dvvv "$signed_path" 2>&1)
  grep -Fqx "Authority=$expected_identity" <<<"$signature"
  grep -Fqx "TeamIdentifier=$expected_team" <<<"$signature"
  grep -Eq '^CodeDirectory .*flags=.*\([^)]*runtime[^)]*\)' <<<"$signature"
  if grep -Fqx 'Signature=adhoc' <<<"$signature"; then
    echo "ad-hoc signature found: $signed_path" >&2
    return 1
  fi
}

verify_signature "$app"
while IFS= read -r signed_path; do
  verify_signature "$signed_path"
done < <(
  find "$app/Contents/Frameworks" \
    \( -type d \( -name '*.app' -o -name '*.framework' \) \
    -o -type f \( -name '*.dylib' -o -perm -111 \) \) -print
)

designated_requirement=$(codesign -d -r- "$app" 2>&1)
grep -F "identifier \"$expected_bundle_id\"" <<<"$designated_requirement" >/dev/null
grep -F "certificate leaf[subject.OU] = $expected_team" <<<"$designated_requirement" >/dev/null

if [[ "$require_notarized" == 1 ]]; then
  spctl --assess --type execute --verbose=4 "$app"
  xcrun stapler validate "$app"
  if command -v syspolicy_check >/dev/null 2>&1; then
    syspolicy_check distribution "$app"
  fi
fi

echo "verified ClickClack $version: $expected_bundle_id, $expected_team, hardened runtime, sealed resources"
