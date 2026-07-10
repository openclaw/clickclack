---
read_when:
  - changing the websocket endpoint, event hub, or cursor logic
  - adding a new durable event type
  - touching reconnect/recovery semantics
---

# Realtime

The realtime layer is a notification pipe over WebSocket plus a recovery
endpoint over HTTP. SQLite is the source of truth; the websocket is allowed
to drop events.

## Components

- `apps/api/internal/realtime/hub.go` — in-process pub/sub keyed by
  `workspace_id`. Buffered per-subscriber channel (32 events) with non-blocking
  send.
- `events` table — append-only log scoped to a workspace, with a sortable
  `cursor`.
- `event_recipients` table — optional per-event recipient rows for durable
  private events such as DMs and read receipts.
- `httpapi.websocket` — accepts a connection, validates membership, drains
  backlog from `events`, then forwards live publishes from the hub.

## Endpoints

```http
GET  /api/realtime/ws?workspace_id=&after_cursor=
GET  /api/realtime/events?workspace_id=&after_cursor=&limit=
POST /api/realtime/ephemeral
```

- `GET /ws` upgrades to a WebSocket. On connect it backfills up to 500
  durable events newer than `after_cursor`, then streams live publishes until
  the client disconnects. Membership is rechecked on every connect.
- `GET /events` is the same backfill in pull form. Use it after a long offline
  period instead of relying on the connect-time backfill. User-private durable
  events, such as read receipts, are filtered the same way as the WebSocket
  stream.
- `POST /ephemeral` publishes a non-durable typing, presence, or agent progress
  event into the hub. Channel events are scoped by `channel_id`; DM events must
  send `direct_conversation_id` and are delivered only to that conversation's
  members.

## Event shape

```jsonc
{
  "id":           "evt_...",
  "cursor":       "...",                 // sortable; opaque to clients
  "type":         "message.created",
  "workspace_id": "wsp_...",
  "channel_id":   "chn_...",             // omitted for workspace-wide events
  "seq":          124,                   // present when tied to channel_seq
  "created_at":   "2026-05-08T12:00:00Z",
  "payload":      { /* type-specific */ }
}
```

## Durable events

Inserted in the same transaction as the underlying mutation:

- `channel.created`, `channel.updated`
- `message.created`, `message.updated`, `message.deleted`
- `channel.read`, `dm.read`
- `thread.reply_created`, `thread.state_updated`
- `reaction.added`, `reaction.removed`
- `member.moderation_updated`

Direct messages also publish into the workspace event stream so DM lists stay
fresh, but they are persisted with recipient rows and replay only to direct
conversation members.

`message.created` carries the message sequence in top-level `seq` and includes
`message_id`, `author_id`, optional `direct_conversation_id`, and optional
`nonce` in `payload`. `message.created` and `thread.reply_created` also include
the request's validated `correlation_id` when one is available. This metadata
survives both cursor replay and live WebSocket delivery; it is omitted for
events created outside a correlated request and never contains message bodies.
Read receipt events carry the updated read pointer in
top-level `seq` and include `user_id` plus the channel or DM conversation ID in
`payload`; they are delivered only to that user.
Moderation events carry the target `user_id` and current `role`; they are
private to the target user and current owners/moderators.

## Ephemeral events

Not persisted, not delivered after disconnect, may be dropped under load:

- `typing.started`
- `typing.stopped`
- `presence.changed`
- `agent.progress`

For DM typing and progress, the server verifies the sender is in the direct
conversation and filters WebSocket delivery to that member set. Workspace
members outside the DM do not receive the event. `agent.progress` is bot-only
and must name exactly one target, so progress from a private agent turn cannot
fall back to a workspace-wide broadcast.

`POST /api/realtime/ephemeral` validates workspace membership and tags the
payload with `user_id` from the caller before publishing.

## Recovery rules

- The client sends `after_cursor` on every connect/reconnect.
- Server returns up to 500 durable events with a higher `cursor`. Anything
  older than that window must be re-fetched through the HTTP API
  (`/messages`, `/thread`, `/channels`) — clients should treat the gap as
  "resync_required".
- The websocket itself does not drop durable events — they are always in
  `events`. A buffered hub channel that overflows simply stops receiving live
  events; the next reconnect with `after_cursor` will fill in.
- Operators can prune old durable events with
  `clickclack admin events prune`. Message history is not stored in the event
  log, so clients with cursors outside the retained window should reload
  through the message APIs.

## Implementation pointers

- `coder/websocket` is the WebSocket library. The accept call validates
  `Origin` against the request host and configured public URL.
- The hub is single-process. Multi-node fanout is out of V1 scope.
