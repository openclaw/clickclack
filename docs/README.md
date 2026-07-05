---
title: ClickClack
description: A self-hostable chat server with Slack-style threads, an OpenAPI-first surface, embedded SQLite, and a TypeScript SDK. One Go binary. Bring your own pincers.
permalink: /
---

# Tiny chat. Big claws.

ClickClack is a self-hostable chat server that fits in a single Go binary. SQLite by
default, an embedded Svelte SPA, Slack-style threads, durable realtime over a
WebSocket pipe, and a framework-neutral TypeScript SDK so the bots feel at home.

It's built for small teams, internal tools, communities, and anyone who would
rather host their own.

Product domain: [clickclack.chat](https://clickclack.chat). App domain:
[app.clickclack.chat](https://app.clickclack.chat), with `/app` as the local
path. Docs domain: [docs.clickclack.chat](https://docs.clickclack.chat).

## Why ClickClack

- **One binary, zero ceremony.** Drop it on a box, point at a data directory,
  reverse-proxy if you want TLS. SQLite ships inside.
- **Realtime that recovers.** WebSocket is a pipe; SQLite is the truth. Reconnect
  with a cursor and you're back, even after a long offline.
- **Threads that don't nest.** One level deep, on purpose. Discussion stays
  scannable.
- **Bots that aren't second-class.** Same auth surface as humans, a typed
  TypeScript SDK, a scriptable CLI client, and a Mattermost-shaped webhook for
  drop-in scripts.
- **Crustacean, lightly.** Lobster mascot, claw reactions, `:molting:` status.
  Normal controls stay normal.

## Five-minute test drive

```sh
pnpm install
pnpm build
go run ./apps/api/cmd/clickclack serve
# open http://localhost:8080
```

The dev fallback boots a default user, workspace, and channel so the SPA loads
into something useful at `/app`. The root path is the product website. Disable
it for anything that isn't a local clone.

[Get the full quickstart →](quickstart.html)

## What's in the box

| Feature | Doc |
| --- | --- |
| Channels, messages, edits, soft-delete | [Messages](features/messages.md) |
| Slack-style threads, one level deep | [Threads](features/threads.md) |
| Reactions on every message | [Reactions](features/reactions.md) |
| Realtime over WebSocket with cursor recovery | [Realtime](features/realtime.md) |
| SQLite FTS5 full-text search | [Search](features/search.md) |
| Local file uploads + message attachments | [Uploads](features/uploads.md) |
| Workspace-scoped direct messages | [Direct messages](features/dms.md) |
| Human and bot identities | [Bots](features/bots.md) |
| Creating and installing bot tokens | [Bot installs](bot-installs.md) |
| Profile settings, handles, avatars | [Profiles](features/profiles.md) |
| Magic-link auth, GitHub OAuth, dev fallback | [Auth](features/auth.md) |
| Native macOS, Windows, and Linux clients | [Desktop apps](desktop.md) |
| Guest waiting-room roles, approvals, timeouts, blocks | [Moderation](features/moderation.md) |
| Mattermost-shaped webhooks and slash commands | [Integrations](features/integrations.md) |
| TypeScript SDK + bot example | [SDK](sdk.md) |

## Operate it

- [CLI](cli.md) — server, admin, backup/export, and remote chat client
  commands.
- [Agent-friendly CLI](agent-friendly-cli.md) — scriptable chat client commands
  for humans, agents, and CI jobs.
- [Configuration](configuration.md) — flag/env/file precedence.
- [Deployment](deployment.md) — single binary, Docker, data layout, OAuth.
- [Bot installs](bot-installs.md) — create bot identities and wire their
  tokens into OpenClaw or SDK runtimes.
- [Development](development.md) — pnpm scripts, monorepo layout, gates.
- [Desktop apps](desktop.md) — native behavior, self-hosted connections, builds, and security.
- [Releasing](releasing.md) — GoReleaser targets, artifacts, and tag flow.

## Look under the hood

- [Architecture](architecture/overview.md) — durable vs realtime, where each
  layer lives.
- [API overview](api/overview.md) — REST/WebSocket surface, auth headers.
- [Data model](data-model.md) — tables, IDs, thread invariants.
- [SPEC.md](https://github.com/openclaw/clickclack/blob/main/SPEC.md) — locked
  V1 decisions, milestones, open questions.

## Status

V1 is in flight. The vertical slice — workspaces, channels, Markdown messages,
threads, realtime, reactions, search, uploads, DMs, magic-link auth, GitHub
OAuth, guest-room moderation, Postgres, R2 uploads, and Docker — is
implemented. Multi-node websocket fanout, federation, voice/video, and full
Mattermost compatibility are intentionally out of scope.

Made with ✦ and a little brine. The lobster is on duty.
