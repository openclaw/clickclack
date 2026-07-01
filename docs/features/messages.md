---
read_when:
  - changing message create/edit/delete or pagination
  - touching the `messages` table or `channel_seq`
  - changing the Markdown body format
---

# Messages

Channel messages are the core durable object. Every message is Markdown text
with optional attachments. Threads are modelled as messages with a non-null
`parent_message_id` (see [threads.md](threads.md)). Inline quote-replies live
on the same row via `quoted_message_id` and friends, documented in
[replies.md](replies.md).

## Endpoints

```http
GET    /api/channels/{channel_id}/messages?after_seq=&before_seq=&around_seq=&limit=
POST   /api/channels/{channel_id}/messages
POST   /api/channels/{channel_id}/read
GET    /api/messages/{message_id}
PATCH  /api/messages/{message_id}
DELETE /api/messages/{message_id}
```

- `GET` returns root messages only (`parent_message_id IS NULL`) for the
  channel, ordered by `channel_seq` ascending. `after_seq` and `before_seq` are
  exclusive cursor windows; `around_seq` returns context around a target
  sequence. Cursor params are mutually exclusive, and `limit` is clamped to
  `1..200` (default 100). Every returned root includes `thread_state`, including
  a zero-reply state, so clients can render thread activity without fetching
  each thread.
- `POST /messages` accepts `{body, quoted_message_id?, nonce?, topic_id?}`.
  Empty bodies are rejected. `nonce` is an optional client idempotency key;
  replaying the same nonce with the same body, quote, and topic returns the
  existing message with HTTP 200 instead of creating a duplicate.
- `POST /read` accepts `{seq}` and updates the caller's monotonic read pointer
  for the channel. The server caps `seq` to the channel's current last root
  message sequence.
- `GET /api/messages/{message_id}` returns a single message visible to the
  current user. DM messages require direct conversation membership.
- `PATCH` accepts `{body}` and only the original author can edit. Sets
  `edited_at`.
- `DELETE` is a soft delete — sets `deleted_at`, keeps the row and the
  `channel_seq` slot so cursors stay valid.

Message create, edit, delete, and read updates emit durable events:
`message.created`, `message.updated`, `message.deleted`, `channel.read`.
Read events are private to the user who advanced the pointer.

## Topics

Topics are optional labels for channel messages. They are useful for deploys,
incidents, customer threads, or other lightweight organization without turning
the channel model into nested rooms.

```http
GET  /api/workspaces/{workspace_id}/topics
POST /api/workspaces/{workspace_id}/topics
```

`POST /topics` accepts `{name, channel_id?}`. A topic without `channel_id` can
be used by any channel in the workspace. A channel-scoped topic can only be
used when posting to that channel. Message responses include `topic_id` when a
topic was supplied.

## Sequence numbers

Every channel message gets a per-channel `channel_seq` assigned inside the
insert transaction:

```sql
SELECT COALESCE(MAX(channel_seq), 0) + 1
FROM messages
WHERE channel_id = ? AND parent_message_id IS NULL
```

That sequence is what clients page by, what the realtime event carries, and
what reconnect uses to backfill. It is monotonic per channel but not globally.
Thread replies use a separate `thread_seq` instead.

## Body format

Bodies are stored as Markdown text. The `body_format` column is hard-coded to
`markdown` in V1 and exists so a future format (rich text, plain) can be added
without a migration. The frontend renders a sanitized subset.

The web composer is a Slack-like message well with a format bar for bold,
italic, inline code, code blocks, links, attachments, and GIF insertion. The GIF
picker inserts standard Markdown image syntax, so no provider-specific durable
schema is required for V1.

## Attachments

Messages carry zero or more attachments via the `message_attachments` join
table. Hydration happens in `hydrateAttachments` and surfaces as the
`attachments` field on `Message`. See [uploads.md](uploads.md) for the
two-step upload-then-attach flow.

The web client renders image, video, audio, PDF, and text attachments as
compact preview cards where safe, and links other attachments as authenticated
download cards. Clicking an inline image attachment, or an image inside
rendered Markdown, opens an in-app image viewer with an Open original link.
Markdown image URLs, including animated GIF URLs, render inline through the
same sanitized Markdown path.

Giphy-backed Markdown GIF images play briefly, then swap to a still preview
with a small replay button in the lower-right corner. Pressing replay reloads
the animated GIF and repeats the same play-once behavior. Other GIF URLs render
normally when no still preview is available.

## Author hydration

`ListMessages` and `GetThread` join `users` and populate `Message.author` so
clients don't need a second round-trip. Avatar URLs are passed through as-is.

## What is intentionally missing

- Hard delete. The soft-delete row stays for cursor stability.
- Pinning and bookmarks.
- Per-message permissions beyond "the author can edit/delete".
