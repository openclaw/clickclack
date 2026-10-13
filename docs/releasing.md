---
read_when:
  - cutting a ClickClack release
  - changing GoReleaser, package artifacts, or release automation
---

# Releasing

ClickClack uses GoReleaser v2. The config is `.goreleaser.yml`; the GitHub
Actions publisher is `.github/workflows/release.yml`.

## Local Smoke Test

```sh
pnpm install
goreleaser check
CLICKCLACK_WEB_VERSION="$(git rev-parse --short=12 HEAD)" \
  goreleaser release --snapshot --clean
```

The snapshot build runs `pnpm build`, then cross-compiles `clickclack` for:

- `linux/amd64`
- `linux/arm64`
- `windows/amd64`
- `windows/arm64`
- `freebsd/amd64`
- `freebsd/arm64`

It also emits Linux `.deb` and `.rpm` packages and `sha256sums.txt`.

The `darwin/amd64` and `darwin/arm64` server archives are built and signed by
the local macOS release command below. GoReleaser excludes them so it cannot
replace signed artifacts with unsigned builds. Server binaries require macOS
13 (the pinned Go toolchain's minimum); the Electron desktop app retains its
macOS 12 minimum.

The same workflow builds Windows and Linux desktop installers on their matching
GitHub runner. The desktop app version comes from the release tag, and each
runner emits a platform checksum manifest:

- Windows: x64 NSIS `.exe` and `.zip`
- Linux: x64 `.AppImage` and `.deb`

Official macOS artifacts are built locally from the exact signed tag on an
authorized maintainer Mac. The Foundation Developer ID private key never enters
GitHub Actions. Electron Builder signs the app and all nested Electron code
inside-out with `Developer ID Application: OpenClaw Foundation (FWJYW4S8P8)`,
enables the hardened runtime, notarizes and staples each architecture, and then
creates the x64 and arm64 `.dmg` and `.zip` files. The local verifier opens every
finished archive and requires the stable `chat.clickclack.desktop` designated
requirement, Foundation team, sealed resources, Gatekeeper acceptance, and a
valid notarization ticket.

The same command builds both macOS server architectures with at most two Go
compiler jobs, signs them with the Foundation identity and hardened runtime,
and submits each archive to Apple. Bare executables cannot be stapled, so both
the local verifier and the clean runner use `codesign --check-notarization -R=notarized` to
require an online notarization ticket. The server checksum manifest and JSON
submission receipts accompany the archives. All macOS signing must run inside
the shared `mac-release codesign-run` helper's managed keychain lock.

GoReleaser leaves the GitHub Release as a draft after uploading the server
artifacts. The publish job downloads the Windows and Linux runner outputs,
verifies every SHA-256 manifest, and attaches them to that draft. A separate
clean macOS runner downloads the pre-uploaded macOS draft assets and repeats
their checksum, signature, Gatekeeper, and notarization verification. The draft
is published only after all of those jobs pass.

## Build the macOS release candidates

Create and verify the signed tag, then check it out in a clean repository. The
notary profile must already be stored in an accessible keychain; its credentials
do not belong in the repository. Set `NOTARYTOOL_KEYCHAIN_PATH` to select a managed
release keychain explicitly on a headless host with a locked login keychain.

```sh
git tag -s v0.7.0 -m "Release v0.7.0"
git push origin main
git push origin v0.7.0
git checkout v0.7.0
NOTARYTOOL_KEYCHAIN_PROFILE=<approved-profile> \
  mac-release codesign-run -- \
  pnpm --filter @clickclack/desktop run dist:mac:release v0.7.0
```

The command fails closed unless `HEAD` is the clean, trusted signed tag. It
leaves the verified files and `ClickClack-<version>-mac-SHA256SUMS.txt` under
`apps/desktop/release/`.

Create a private draft containing those files:

```sh
gh release create v0.7.0 --draft --verify-tag \
  apps/desktop/release/ClickClack-0.7.0-mac-*.dmg \
  apps/desktop/release/ClickClack-0.7.0-mac-*.zip \
  apps/desktop/release/ClickClack-0.7.0-mac-SHA256SUMS.txt \
  apps/desktop/release/clickclack_0.7.0_darwin_*.tar.gz \
  apps/desktop/release/ClickClack-0.7.0-mac-server-SHA256SUMS.txt \
  apps/desktop/release/ClickClack-0.7.0-mac-server-notarization.json
```

## Publish

Run the `release` workflow from protected `main` and provide the existing tag.
The workflow checks out the tag, installs Go and pnpm, runs `pnpm check`, sets
`CLICKCLACK_WEB_VERSION` to the checked-out commit, then runs
`goreleaser release --clean` with `GITHUB_TOKEN`. Release notes come from the
matching version section in `CHANGELOG.md`, preserving its highlights and order
in the GitHub Release; a missing or empty section stops publication. In parallel, native runners
build Windows and Linux desktop apps and a clean macOS runner verifies the
signed draft assets. Once every job succeeds, the verified desktop files are
attached and the draft is published.

GoReleaser reuses the existing draft and replaces matching server assets, while
the desktop uploader replaces matching Windows and Linux assets. This makes a
failed draft release safe to retry without making published releases mutable.

If local packaging produced all archives but a verifier defect stopped completion,
preserve those candidates and fix the verifier through normal review and CI on
protected `main`. Verify the preserved artifacts with that trusted checkout,
then finish the desktop checksum manifest before creating the draft:

```sh
"$VERIFY_TREE/apps/desktop/scripts/verify-macos-release.sh" v0.7.0 "$RELEASE_DIR"
node "$VERIFY_TREE/apps/desktop/scripts/release-artifacts.mjs" mac 0.7.0 "$RELEASE_DIR"
```

`VERIFY_TREE` is the clean, reviewed `main` checkout and `RELEASE_DIR` contains the
artifacts already built from the signed tag. Every signature, notarization, version,
and architecture check must pass. The signed tag and built code remain unchanged;
the workflow independently repeats verification from protected `main`.
