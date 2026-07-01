# ClickClack Spec

ClickClack is a self-hostable, API-first chat app for internal testing, small teams, and communities. It mixes Slack-style productivity with Discord-style warmth, plus a light crustacean theme.

## Goals

- Run as a tiny single binary with first-class SQLite storage.
- Offer a hosted/server deployment path with Postgres later.
- Provide reliable realtime text chat with Slack-style threads.
- Keep the backend API-first and frontend-framework-independent.
- Ship a TypeScript SDK for bots, integrations, and community tooling.
- Feel playful and memorable without sacrificing dense, practical chat workflows.

## Locked V1 Decisions

- First implementation target: realtime channel chat plus Slack-style threads, not skeleton-only.
- Auth starts CLI-manageable: local owner/user bootstrap and invite/token management from `clickclack admin ...`.
- GitHub OAuth is optional V1, after local auth is usable.
- Frontend is Svelte 5 + Vite SPA. No SvelteKit server layer.
- API contract is OpenAPI-first, with `packages/protocol/openapi.yaml` as the source of truth.
- IDs use ULID-style sortable text IDs with semantic prefixes such as `usr_`, `wsp_`, `chn_`, `msg_`, `evt_`.
- Message body format starts as Markdown. Clients render a safe Markdown subset.
- Search, uploads, and DMs are V1 product scope, but come after the realtime channel/thread vertical slice is working.
- Monorepo layout is the canonical repo shape.

## Non-Goals For V1

- Voice/video rooms.
- Full Slack, Discord, or Mattermost server compatibility.
- Federation.
- End-to-end encryption.
- Enterprise compliance features.
- Multi-node websocket fanout.

## Product Shape

### Naming

- Product: ClickClack.
- Primary domain: `clickclack.chat`.
- Backend/protocol codename, if needed: Clawwire.
- Theme: lobster/crustacean accents, not renamed core UX primitives.

### First Users

- Internal testing groups.
- Self-hosted teams.
- Small communities.
- Bot-heavy hacker spaces.

### UX Model

- Multi-workspace.
- Workspace contains channels.
- Channel timeline shows root messages only.
- Every root message can have one Slack-style thread.
- Thread opens in a right-side pane.
- Thread replies are one-level only; no nested reply trees.
- Presence and typing are ephemeral.
- Light/dark themes from day one.

Use familiar terms for core navigation:

- Workspace
- Channel
- Thread
- Message
- Reaction
- Bot

Use crustacean flavor in:

- Logo/mascot.
- Empty states.
- Loading states.
- Reaction pack.
- Sounds.
- Onboarding copy.
- Optional statuses like `molting`, `lurking`, `afk`.

## V1 Vertical Slice

The first useful build should support:

- Create/select workspace.
- Create/select channel.
- Send Markdown text message.
- Realtime message delivery over WebSocket.
- Open message thread in right pane.
- Send thread reply.
- Persist everything in SQLite.
- Reload/reconnect and recover state.
- CLI-manageable local auth/bootstrap.
- Embedded web app served by Go.

After that vertical slice is stable, V1 expands to:

- Direct messages.
- SQLite FTS5 message search.
- Local file uploads and message attachments.
- GitHub OAuth as an optional login path.

## Architecture

```text
clickclack/
  apps/
    api/              # Go backend and single-binary entrypoint
    web/              # Svelte SPA
  packages/
    protocol/         # OpenAPI spec and event schemas
    sdk-ts/           # TypeScript SDK, generated client + friendly wrapper
  docs/
    architecture/
    api/
  infra/
    migrations/
      sqlite/
      postgres/       # later
```

## Backend

Language: Go.

Initial runtime:

- Single Go process.
- `modernc.org/sqlite`.
- Embedded migrations.
- Embedded Svelte build via `go:embed`.
- Local upload storage.
- In-process websocket hub.

Future hosted runtime:

- Postgres.
- Object storage.
- External queue/pubsub only when needed.
- Multi-node websocket fanout later.

### Suggested Go Libraries

- HTTP router: `chi`.
- SQLite: `modernc.org/sqlite`.
- Postgres later: `pgx`.
- Queries: start handwritten or `sqlc` once schema settles.
- Migrations: embedded SQL migrations with a tiny internal runner, or `goose` if the runner grows.
- IDs: ULID-style sortable text IDs with type prefixes.

### CLI

```text
clickclack serve
  --addr :8080
  --data ./data
  --db sqlite://./data/clickclack.db

clickclack migrate
  --db sqlite://./data/clickclack.db

clickclack admin bootstrap
  --name "Peter"
  --email steipete@gmail.com

clickclack admin user create
  --name "Ari"
  --email ari@example.com

clickclack admin invite create
  --workspace wsp_...
```

Default `clickclack serve` should be enough for local development. Production-like local use should bootstrap an owner through the CLI before exposing the instance.

## Frontend

Framework: Svelte 5 SPA.

Use plain Svelte + Vite unless SvelteKit offers clear value without adding server-side complexity. The Go server owns HTTP/API/auth and serves static assets.

Frontend responsibilities:

- Render workspace/channel/thread UI.
- Keep local client cache/projection.
- Use HTTP API for writes and fetches.
- Use WebSocket for realtime events.
- Recover by refetching from API after reconnect.

Frontend should not own durable chat truth.

## API

Contract: OpenAPI first.

Source of truth:

```text
packages/protocol/openapi.yaml
```

Generate:

- Go request/response types or validators where useful.
- TypeScript API client.
- SDK docs.

Initial REST shape:

```text
GET    /api/me

GET    /api/workspaces
POST   /api/workspaces
GET    /api/workspaces/{workspace_id}

GET    /api/workspaces/{workspace_id}/channels
POST   /api/workspaces/{workspace_id}/channels
PATCH  /api/channels/{channel_id}

GET    /api/channels/{channel_id}/messages?before=&after_seq=&limit=
POST   /api/channels/{channel_id}/messages
PATCH  /api/messages/{message_id}
DELETE /api/messages/{message_id}

GET    /api/messages/{message_id}/thread
POST   /api/messages/{message_id}/thread/replies

POST   /api/messages/{message_id}/reactions
DELETE /api/messages/{message_id}/reactions/{emoji}

GET    /api/realtime/events?after_cursor=
POST   /api/realtime/ephemeral
GET    /api/realtime/ws

GET    /api/search?workspace_id=&q=&limit=

POST   /api/uploads
GET    /api/uploads/{upload_id}

GET    /api/dms
POST   /api/dms
GET    /api/dms/{conversation_id}
DELETE /api/dms/{conversation_id}
POST   /api/dms/{conversation_id}/open
GET    /api/dms/{conversation_id}/messages?before=&after_seq=&limit=
POST   /api/dms/{conversation_id}/messages
```

## Realtime

Realtime must be recoverable.

Rules:

- WebSocket is a notification/update pipe.
- SQLite/Postgres is source of truth.
- Every durable event is recoverable through HTTP.
- Client reconnects with last seen cursor.
- If cursor is too old or unknown, server returns `resync_required`.

Send flow:

1. Client calls `POST /api/channels/{id}/messages`.
2. Server validates auth and membership.
3. Server transaction:
   - insert message
   - assign per-channel sequence
   - insert event into outbox/events table
   - update thread/channel summary state
4. In-process dispatcher broadcasts event to websocket subscribers.
5. Client reconciles optimistic message with server event.

Event shape:

```json
{
  "id": "evt_...",
  "cursor": "...",
  "type": "message.created",
  "workspace_id": "w_...",
  "channel_id": "c_...",
  "seq": 124,
  "created_at": "2026-05-08T12:00:00Z",
  "payload": {
    "message_id": "m_..."
  }
}
```

Initial durable events:

- `message.created`
- `message.updated`
- `message.deleted`
- `thread.reply_created`
- `thread.state_updated`
- `reaction.added`
- `reaction.removed`
- `channel.created`
- `channel.updated`

Ephemeral events:

- `typing.started`
- `typing.stopped`
- `presence.changed`

Ephemeral events are not persisted and may be dropped.

## Data Model

Initial tables:

```text
users
  id
  display_name
  avatar_url
  created_at

identities
  id
  user_id
  provider
  provider_subject
  email
  created_at

workspaces
  id
  name
  slug
  created_at

workspace_members
  workspace_id
  user_id
  role
  created_at

channels
  id
  workspace_id
  name
  kind
  created_at
  archived_at

messages
  id
  workspace_id
  channel_id
  author_id
  parent_message_id
  thread_root_id
  channel_seq
  thread_seq
  body
  body_format
  created_at
  edited_at
  deleted_at

thread_state
  root_message_id
  reply_count
  last_reply_at
  last_reply_author_ids_json

reactions
  message_id
  user_id
  emoji
  created_at

events
  id
  cursor
  workspace_id
  channel_id
  type
  payload_json
  created_at

uploads
  id
  workspace_id
  owner_id
  filename
  content_type
  byte_size
  storage_path
  created_at

message_attachments
  message_id
  upload_id
  created_at

direct_conversations
  id
  workspace_id
  created_at

direct_conversation_members
  conversation_id
  user_id
  created_at
```

Thread rules:

- Root message has `parent_message_id = null`.
- Root message has `thread_root_id = id`.
- Thread reply has `parent_message_id = root_message_id`.
- Thread reply has `thread_root_id = root_message_id`.
- No nested replies in V1.

## Storage

SQLite is first-class.

SQLite requirements:

- Use `modernc.org/sqlite`.
- Enable WAL mode.
- Use a single writer discipline.
- Keep transactions short.
- Prefer portable SQL.
- Avoid Postgres-only behavior in core paths.
- Add separate Postgres migrations later rather than forcing one dialect.

Local file layout:

```text
data/
  clickclack.db
  uploads/
  logs/
```

## Auth

V0:

- CLI owner bootstrap.
- CLI user/invite management.
- Dev/local auth for quick testing, gated to local/dev mode.
- CLI-generated magic-link tokens.
- Bearer session tokens and HTTP-only cookie sessions.

V1:

- Magic-link token issuance and consume flow, with CLI/local delivery first.
- GitHub OAuth as optional login, enabled via self-host config.
- SMTP or provider-backed email delivery later, once deployment mail settings are known.
- Optional local email/password only if needed for fully offline/self-hosted deployments.

Auth principles:

- Workspace membership checked on every API write.
- WebSocket subscribe validates workspace/channel access.
- Recheck permissions for channel/thread fetches.

## SDK

First SDK: TypeScript.

Location:

```text
packages/sdk-ts
```

Layering:

- Generated OpenAPI types.
- Friendly wrapper.
- WebSocket/event subscription helper.

Example API:

```ts
const client = new ClickClackClient({ baseUrl, token });

await client.channels.sendMessage(channelId, {
  body: "click clack",
});

client.events.subscribe({
  workspaceId,
  onEvent(event) {
    // handle event
  },
});
```

SDK must not depend on Svelte.

## Mattermost Compatibility

Do not clone the full Mattermost API in V1.

Do support:

- Incoming webhook compatibility.
- Simple slash-command callback shape.
- Import helpers for exports if useful.

Do not support early:

- Existing Mattermost clients connecting directly.
- Full REST API compatibility.
- Full permission/model compatibility.

## Design Direction

ClickClack should feel:

- Fast.
- Dense.
- Friendly.
- Slightly weird.
- More polished tool than joke app.

Visual direction:

- Light and dark themes.
- Neutral UI base.
- Coral, shell, brine, ink accents.
- Crustacean mascot and iconography used sparingly.
- Avoid novelty typography.
- Avoid making normal controls hard to understand.

UI layout:

```text
left sidebar: workspaces / channels
center: channel timeline
right pane: thread
bottom: composer
top: channel title, members, search
```

## Development Milestones

### M0: Skeleton

- Monorepo.
- Go server boots.
- Svelte app builds.
- Go embeds and serves web assets.
- SQLite opens and migrates.

### M1: Durable Chat

- Workspaces/channels/messages schema.
- REST create/list messages.
- Basic dev auth.
- Message timeline UI.

### M2: Realtime

- WebSocket endpoint.
- Event outbox.
- Live message updates.
- Reconnect and cursor recovery.

### M3: Threads

- Root messages and one-level replies.
- Thread pane.
- Thread reply counts and last reply state.

### M4: Search, Uploads, DMs

- SQLite FTS5 message search.
- Local upload storage.
- Message attachments.
- Direct message conversations.

### M5: Self-Host Polish

- First-run owner setup.
- CLI-generated magic-link auth.
- Config file/env.
- Docker image.
- Backups/export.

### M6: SDK And Integrations

- OpenAPI generation.
- TypeScript SDK.
- Incoming webhooks.
- Basic bot example.

## Answered Questions

- Setup starts with CLI owner bootstrap. A setup UI can be added later.
- Markdown is the initial rich text format.
- DMs are V1 scope, after channel chat and threads.
- Search starts with SQLite FTS5.
- Uploads are V1 scope, after core chat is solid.
- OpenAPI remains source of truth from the first scaffold.
- TypeScript compilation uses `tsgo`; lint/format use `oxlint` and `oxfmt`.
- GitHub OAuth ships in V1 as an optional configured auth provider.

## Open Questions

- Whether to add generated Go request/response validation from OpenAPI in V1 or keep the first backend on hand-written handlers.
