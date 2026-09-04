# ClickClack 🦞 — Tiny chat. Big claws.

[![CI](https://img.shields.io/github/actions/workflow/status/openclaw/clickclack/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/openclaw/clickclack/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/openclaw/clickclack?style=flat-square)](https://github.com/openclaw/clickclack/releases/latest)
[![License](https://img.shields.io/github/license/openclaw/clickclack?style=flat-square)](LICENSE)
[![Docs](https://img.shields.io/badge/docs-clickclack.chat-5b5bd6?style=flat-square)](https://docs.clickclack.chat)

![ClickClack banner](docs/assets/readme-banner.jpg)

ClickClack is self-hostable team chat for people, bots, and agents. It combines a Go server, Svelte web app, scriptable CLI, and TypeScript SDK behind one API-first chat service.

![ClickClack chat](docs/proof/switchboard-after-chat-light.png)

## Install

The smallest path is the hosted service: open **[app.clickclack.chat](https://app.clickclack.chat)** in a browser.

For a desktop client or self-hosted server, download the matching artifact from the **[latest GitHub release](https://github.com/openclaw/clickclack/releases/latest)**. Releases contain desktop installers for macOS, Windows, and Linux, plus server archives or packages for macOS, Linux, Windows, and FreeBSD.

For source builds and package details, see the [install guide](docs/install.md).

## Quick start

After downloading the `clickclack` server binary, start a local instance:

```sh
./clickclack serve --dev-bootstrap=true
```

Open [http://localhost:8080/app](http://localhost:8080/app). On an empty data directory, development bootstrap creates a local owner, workspace, and channel; data stays in `./data` by default.

The [full quickstart](docs/quickstart.md) continues with real accounts, sessions, channels, and a bot.

## What runs where

The server is one Go binary with the web app, SQLite migrations, and static assets embedded. SQLite and local uploads are the defaults; Postgres and Cloudflare R2 are available for deployments that need external storage.

Durable chat state and the event log live in the database. WebSockets carry live updates, and clients recover missed events with cursors after reconnecting.

| Surface | Use it for | Guide |
| --- | --- | --- |
| Web and desktop | Channels, threads, search, uploads, direct messages, and moderation | [Feature index](docs/README.md#whats-in-the-box) |
| CLI | Server administration, backups, exports, and scripted chat | [CLI reference](docs/cli.md) |
| TypeScript SDK | Typed HTTP and realtime clients for bots and integrations | [SDK guide](docs/sdk.md) |
| REST and WebSocket API | Clients in other languages and direct integrations | [API overview](docs/api/overview.md) |

## Self-hosting

ClickClack can run from a release binary, a multi-stage Docker image built from this repository, or a source build. Production deployments disable development bootstrap and configure authentication, storage, TLS termination, and backups explicitly.

Start with the [deployment guide](docs/deployment.md), then use the [configuration reference](docs/configuration.md) for flag, environment, and file precedence. The [architecture overview](docs/architecture/overview.md) describes the server, storage, realtime, and embedded frontend boundaries.

## Documentation

The complete documentation is at **[docs.clickclack.chat](https://docs.clickclack.chat)** and in [`docs/`](docs/).

- [Features and operations](docs/README.md)
- [Authentication](docs/features/auth.md)
- [Bots and integrations](docs/features/bots.md)
- [Desktop apps](docs/desktop.md)
- [Data model](docs/data-model.md)
- [Development](docs/development.md)

## Development

Source builds use Go 1.27.1, Node.js 24 or newer, and pnpm 11.25.0. The module minimum remains Go 1.26.6 so managed CodeQL can build with its preinstalled compiler; the `toolchain` directive selects Go 1.27.1 for normal builds.

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm check
```

See the [development guide](docs/development.md) for the two-process web loop, repository layout, focused scripts, and test details.

## License

[MIT](LICENSE).
