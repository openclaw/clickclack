---
read_when:
  - changing the websocket endpoint, event hub, or cursor logic
  - adding a new durable event type
  - touching reconnect/recovery semantics
---

# Realtime

The realtime layer is a notification pipe over WebSocket plus a recovery
endpoint over HTTP. The durable log in SQLite or PostgreSQL is the source of
truth for both replay and live delivery; the hub only wakes its readers.

## Components

- `apps/api/internal/realtime/hub.go` — in-process pub/sub keyed by
  `workspace_id`. Durable publications coalesce into one pending wake per
  subscriber, without carrying private payloads. Cursorless ephemeral events
  use a separate 32-event buffer; overflow closes only the slow subscriber.
- `events` table — append-only log scoped to a workspace, with a sortable
  `cursor`.
- `event_recipients` table — optional per-event recipient rows for durable
  private events such as DMs and read receipts.
- `httpapi.websocket` — accepts a connection, validates membership, drains
  `events` in cursor order on startup and on each durable wake. It forwards
  authorized cursorless ephemeral events directly from the hub.

## Endpoints

```http
GET  /api/realtime/ws?workspace_id=&after_cursor=
GET  /api/realtime/events?workspace_id=&after_cursor=&limit=&include_tail=
POST /api/realtime/ephemeral
```

- `GET /ws` upgrades to a WebSocket. On connect it captures the latest visible
  durable-event cursor, pages forward from `after_cursor` until reaching that
  fixed tail. Each live wake repeats that same drain with a new finite tail;
  a wake arriving during a drain stays pending. Each drain is capped at 5,000 events; larger
  gaps close with application code `4001` so the client can perform an
  authoritative HTTP resync. Membership is checked on connect and current
  access is rechecked before delivery, as is the session the connection
  authenticated with: a password change or a sign-out that revokes it closes
  the socket with `1008` rather than letting an already-open connection keep
  receiving. That check reads the store, so it holds across replicas, and an
  idle connection repeats it on a timer. A temporary session-store failure closes
  with retryable code `1013`; HTTP session checks return `503`, preserving the
  session so clients can reconnect after recovery. The output-only socket handles ping,
  pong, and close frames and releases its subscription on peer disconnect;
  clients send ephemeral input through HTTP.
- `GET /events` exposes durable replay in pull form. User-private durable
  events, such as read receipts, are filtered the same way as the WebSocket
  stream. Pass `include_tail=true` when a fresh client needs to skip retained
  history: the response adds `tail_cursor`, captured before the page query, and
  the client can open `/ws` from that cursor without racing events created
  during startup. Servers that predate this option omit the field.
- `POST /ephemeral` publishes a non-durable typing, presence, or agent progress
  event into the hub. Channel events are scoped by `channel_id`; DM events must
  send `direct_conversation_id` and are delivered only to that conversation's
  members.

## Event shape

```jsonc
{
  "id": "evt_...",
  "cursor": "...", // sortable; opaque to clients
  "type": "message.created",
  "workspace_id": "wsp_...",
  "channel_id": "chn_...", // omitted for workspace-wide events
  "seq": 124, // present when tied to channel_seq
  "created_at": "2026-05-08T12:00:00Z",
  "payload": {/* type-specific */},
}
```

## Durable events

Inserted in the same transaction as the underlying mutation:

- `channel.created`, `channel.updated`
- `message.created`, `message.updated`, `message.deleted`
- `channel.read`, `dm.read`
- `thread.reply_created`, `thread.state_updated`
- `reaction.added`, `reaction.removed`
- `pin.added`, `pin.removed`
- `member.moderation_updated`

The common append helper is the transaction's finalization boundary: complete
all domain writes and acquire their locks before the first append. PostgreSQL
locks the referenced workspace and sorted recipient users with `FOR KEY SHARE`
before acquiring a workspace advisory transaction lock. It then reads the
unfiltered persisted frontier in a fresh Read Committed statement. The fence
is held through commit, so a later cursor cannot commit ahead of an earlier
one. Multi-event transactions reuse the workspace and recipients. SQLite's
immediate write transaction provides the equivalent serialization.

Both stores preserve the new ULID candidate's entropy and allocate a cursor
strictly above the workspace frontier, including private events. If a reopened
process or clock adjustment produces a lower candidate, its timestamp is moved
to the frontier millisecond, or one millisecond later if needed; exhaustion
fails the transaction. Cursors are opaque ordering tokens, not wall-clock times.
Ordinary IDs and the event's `created_at` timestamp retain their existing meaning.

Even private publications wake every subscriber in the workspace: an earlier
public event may have committed before its handler published. SQL visibility
filters decide which rows each reader receives. Outgoing subscriptions instead
consume only original mutation receipts, whose recipient metadata excludes
private callbacks. Replaying a socket never invokes an outgoing callback.

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

While a turn is live, the web app resolves that authenticated `user_id` through
the shared workspace identity cache and names the responding agent beside both
channel and thread composers. Agent identity is workspace-visible by design to
members who can receive that channel or DM progress event. Concurrent agents
are keyed by `(user_id, turn_id)`; an unresolved sender is shown as `Agent`.

The TypeScript SDK exports `AgentProgressLine`, `AgentProgressPayload`, and
`EphemeralEventInput`. Its input union requires one target for typing and agent
progress while retaining targetless, workspace-wide presence events.

## Recovery rules

- The web client applies durable data before checkpointing its cursor. Timeline
  scrolling and read receipts settle independently, so suspended animation frames
  in a hidden tab do not block subsequent events. Live chat stays pinned as
  successive messages grow an existing group, including after the tab resumes.
  Scrolling into history cancels pending live following, including after a resize
  or when animation frames resume. Interrupting a history-page restoration still
  allows subsequent pages to load. Centering a message keeps the surrounding app
  layout fixed.
- Channel and DM list snapshots account for unread counts without consuming live
  timeline events. Browser and desktop alerts track received message sequences
  separately, so a list refresh cannot hide a new alert and a retried event does
  not repeat it. Initial snapshots still suppress historical message alerts.
- When following live chat, a snapshot arriving before or after a live message
  does not leave it unread after its row and scroll position settle.
  Replayed loaded rows and history viewed away from the live edge retain their
  existing unread boundaries.
- The client sends `after_cursor` on every connect/reconnect.
  Each attempt first validates access through the HTTP events endpoint, even
  when it has a saved cursor. An expired session returns the chat or embedded
  view to sign-in; a workspace permission error remains distinct from global
  sign-out. Temporary failures retry with the same processed cursor, so they
  cannot skip missed events. Validation retains the usual 30-second request
  deadline and is cancelled when leaving the workspace.
- On WebSocket connect and each live wake, the server pages durable events with
  a higher `cursor` until it reaches the visible tail captured for that drain. If replay is
  interrupted, the client can reconnect with the last cursor it actually
  processed and resume from there. If the 5,000-event work budget is exhausted,
  the server closes with code `4001`; the web client clears its stale cursor,
  captures a fresh tail, completes an authoritative projection resync, and then
  resumes live delivery. Chat and embedded views reset reaction projections
  before hydrating fresh message pages, including every retained thread page.
- A durable burst coalesces wakeups without losing log rows. If a subscriber's
  ephemeral queue overflows, or a socket write cannot finish within its timeout,
  the connection closes so the client can reconnect with its last processed
  `after_cursor`. Ephemeral events are not recoverable.
- Operators can prune old durable events with
  `clickclack admin events prune`. Message history is not stored in the event
  log, so clients with cursors outside the retained window should reload
  through the message APIs.

## Implementation pointers

- `coder/websocket` is the WebSocket library. The accept call validates
  `Origin` against the request host and configured public URL.
- The hub is single-process. Multi-node fanout is out of V1 scope.
