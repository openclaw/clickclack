---
title: Install
description: Ways to get a ClickClack binary running — from-source, Docker, or build-and-copy.
---

# Install

ClickClack ships as a single Go server binary that embeds the Svelte SPA and SQL
migrations, plus optional desktop clients for macOS, Windows, and Linux. There
are three sensible ways to install the server.

## From source

You'll need a recent Go and pnpm 11 (auto-managed via `corepack`).

```sh
git clone https://github.com/openclaw/clickclack.git
cd clickclack
pnpm install
pnpm build
go run ./apps/api/cmd/clickclack serve
# open http://localhost:8080
```

`pnpm build` builds the SPA + the TypeScript SDK and copies
`apps/web/dist` into `apps/api/internal/webassets/dist` so `go:embed` can
bake it into the binary. The Go build will fail without that step.

To produce a standalone binary instead of `go run`:

```sh
pnpm build
go build -o clickclack ./apps/api/cmd/clickclack
./clickclack serve --addr :8080 --data /var/lib/clickclack
```

## Docker

The repo ships a multi-stage `Dockerfile`:

```sh
docker build -t clickclack .
docker run --rm -p 8080:8080 -v clickclack-data:/app/data clickclack
```

The image runs as a non-root `clickclack` user, exposes `8080`, and mounts
`/app/data` as a volume. Override the entrypoint command to run admin
tasks:

```sh
docker run --rm -v clickclack-data:/app/data clickclack \
  admin bootstrap --name "Peter" --email steipete@gmail.com
```

## Pre-built binaries

GitHub releases publish `clickclack` archives for Linux, macOS, Windows, and
FreeBSD on `amd64` and `arm64`, plus Linux `.deb` and `.rpm` packages.

```sh
# macOS/Linux tarball shape
tar -xzf clickclack_<version>_<os>_<arch>.tar.gz
./clickclack version

# Linux packages
sudo dpkg -i clickclack_<version>_amd64.deb
sudo rpm -i clickclack-<version>-1.x86_64.rpm
```

Release archives include `LICENSE`, `README.md`, `SPEC.md`, `CHANGELOG.md`,
and the docs tree. Checksums are published as `sha256sums.txt`.

## What you get

Either path produces the same `clickclack` binary with the SPA, migrations,
and assets baked in. Data lives in whatever directory you pass to `--data`
(default `./data`).

```
<data>/
  clickclack.db
  uploads/
  logs/
```

The desktop clients connect to that server; they do not replace or bundle it.
See [Desktop apps](desktop.html) for native features, self-hosted server setup,
preview installers, and local packaging commands.

## Next

- [Quickstart](quickstart.html) — the first 5 minutes.
- [Configuration](configuration.html) — flags, env vars, config file.
- [Deployment](deployment.html) — reverse proxy, OAuth, backups.
