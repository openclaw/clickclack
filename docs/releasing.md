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
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`
- `windows/arm64`
- `freebsd/amd64`
- `freebsd/arm64`

It also emits Linux `.deb` and `.rpm` packages and `sha256sums.txt`.

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

GoReleaser leaves the GitHub Release as a draft after uploading the server
artifacts. The publish job downloads the Windows and Linux runner outputs,
verifies every SHA-256 manifest, and attaches them to that draft. A separate
clean macOS runner downloads the pre-uploaded macOS draft assets and repeats
their checksum, signature, Gatekeeper, and notarization verification. The draft
is published only after all of those jobs pass.

## Build the macOS release candidates

Create and verify the signed tag, then check it out in a clean repository. The
notary profile must already be stored in the login keychain; its credentials do
not belong in the repository.

```sh
git tag -s v0.3.0 -m "Release v0.3.0"
git push origin main
git push origin v0.3.0
git checkout v0.3.0
NOTARYTOOL_KEYCHAIN_PROFILE=<approved-profile> \
  pnpm --filter @clickclack/desktop run dist:mac:release -- v0.3.0
```

The command fails closed unless `HEAD` is the clean, trusted signed tag. It
leaves the verified files and `ClickClack-<version>-mac-SHA256SUMS.txt` under
`apps/desktop/release/`.

Create a private draft containing those files:

```sh
gh release create v0.3.0 --draft --verify-tag \
  apps/desktop/release/ClickClack-0.3.0-mac-*.dmg \
  apps/desktop/release/ClickClack-0.3.0-mac-*.zip \
  apps/desktop/release/ClickClack-0.3.0-mac-SHA256SUMS.txt
```

## Publish

Run the `release` workflow from protected `main` and provide the existing tag.
The workflow checks out the tag, installs Go and pnpm, runs `pnpm check`, sets
`CLICKCLACK_WEB_VERSION` to the checked-out commit, then runs
`goreleaser release --clean` with `GITHUB_TOKEN`. In parallel, native runners
build Windows and Linux desktop apps and a clean macOS runner verifies the
signed draft assets. Once every job succeeds, the verified desktop files are
attached and the draft is published.

GoReleaser reuses the existing draft and replaces matching server assets, while
the desktop uploader replaces matching Windows and Linux assets. This makes a
failed draft release safe to retry without making published releases mutable.
