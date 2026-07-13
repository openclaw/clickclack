---
read_when:
  - changing tables, IDs, or invariants
  - planning Postgres support
---

# Data Model

SQLite schema lives in `apps/api/internal/store/sqlite/migrations/`. The
Postgres schema lives in `apps/api/internal/store/postgres/migrations/`.
Those embedded migration directories are canonical and append-only.
`infra/migrations/sqlite` is a partial legacy tooling snapshot; it is not a
runtime input and is not maintained as a complete mirror. There is no
`infra/migrations/postgres` mirror.

## IDs

Sortable ULID-style text IDs with semantic prefixes:

| Prefix  | Object |
|---------|--------|
| `usr_`  | user |
| `idn_`  | identity (provider link) |
| `wsp_`  | workspace |
| `chn_`  | channel |
| `msg_`  | message |
| `evt_`  | durable event |
| `eph_`  | ephemeral event (in-memory only) |
| `upl_`  | upload |
| `inv_`  | invite |
| `mlk_`  | magic link |
| `ses_`  | session record |
| `sst_`  | human session bearer token |

Public app URLs use separate immutable random route IDs. They do not replace
the internal IDs above. Workspaces expose `T...`, channels expose `C...`,
direct conversations expose `D...`, and thread root messages expose `M...`
only when a thread URL is needed.

## Tables (V1)

```text
users                              identities
workspaces                         workspace_members
workspace_member_moderation
channels
messages                           thread_state
reactions
events                             auth_magic_links / sessions
event_recipients
uploads                            message_attachments
direct_conversations               direct_conversation_members
invites
messages_fts                       (SQLite FTS5 virtual)
```

Full SQLite SQL is in
[`apps/api/internal/store/sqlite/migrations/0001_initial.sql`](../apps/api/internal/store/sqlite/migrations/0001_initial.sql)
and
[`0002_auth.sql`](../apps/api/internal/store/sqlite/migrations/0002_auth.sql);
later migrations add auth/session hardening, upload metadata, public route IDs,
read receipts, bot records, Postgres parity, and member moderation.
Full Postgres SQL is in
[`apps/api/internal/store/postgres/migrations/0001_schema.sql`](../apps/api/internal/store/postgres/migrations/0001_schema.sql).

## Thread invariants

For any row in `messages`:

- Root: `parent_message_id IS NULL`, `thread_root_id = id`,
  `channel_seq IS NOT NULL`, `thread_seq IS NULL`.
- Reply: `parent_message_id = root.id`, `thread_root_id = root.id`,
  `channel_seq IS NULL`, `thread_seq IS NOT NULL`.
- DM root: `direct_conversation_id IS NOT NULL`, `channel_id IS NULL`,
  `parent_message_id IS NULL`, `channel_seq` used as the per-conversation
  sequence.
- DM reply: `direct_conversation_id IS NOT NULL`, `channel_id IS NULL`,
  `parent_message_id IS NOT NULL`, `channel_seq IS NULL`, `thread_seq IS NOT NULL`.

Nested replies are forbidden — the API rejects replies to non-root messages.

## Sequences

- `channels.channel_seq` (computed): `MAX(channel_seq) + 1` per channel for
  root messages, assigned in the insert tx.
- `thread_root.thread_seq` (computed): `MAX(thread_seq) + 1` per root message
  for replies.
- `events.cursor`: globally sortable opaque cursor used by realtime
  recovery.

## Private durable events

`events.is_private` is the durable privacy bit for replay filtering.
`event_recipients(event_id, user_id)` is the allow-list for private durable
events. Public events have `is_private = 0`; private events have
`is_private = 1` and are returned only to listed users during replay. Recipient
rows cascade when old events are pruned.

## Soft-deletes

Messages set `deleted_at` instead of removing the row. This keeps
`channel_seq`/`thread_seq` stable for cursors and reconnect.

## FTS

SQLite `messages_fts` mirrors `messages.body` with `porter unicode61`. Three
triggers keep it in sync on insert/delete/update-of-body. Postgres search uses
`to_tsvector` / `websearch_to_tsquery` against `messages.body`. See
[features/search.md](features/search.md).

## Postgres path

The store interface in `apps/api/internal/store/types.go` is the abstraction
line — handlers should keep calling store methods, not embed dialect-specific
SQL.

SQL accessors are generated with `sqlc`. When changing store SQL, edit the
schema/query files and run `pnpm generate:sqlc`; do not hand-maintain generated
`storedb` code.
