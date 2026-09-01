---
read_when:
  - changing thread reply behavior or thread state
  - adding nested replies (don't, V1 forbids it)
---

# Threads

Threads are Slack-style: every root message can have one flat list of replies.
Nested replies are explicitly rejected. For lighter-weight inline replies that
keep the new message in the parent stream and just quote what they're
answering, see [replies.md](replies.md).

## Endpoints

```http
GET  /api/messages/{message_id}/thread                    # root + replies + state
POST /api/messages/{message_id}/thread/replies            # body, quote, nonce
```

`GET` returns:

```jsonc
{
  "root":          Message,
  "replies":       Message[],          // ordered by thread_seq asc, capped 1..200 (default 100)
  "thread_state":  ThreadState,        // counters/last reply summary
  "oldest_seq":    1,                  // reply sequence bounds; zero for an empty page
  "newest_seq":    100,
  "has_older":     false,
  "has_newer":     true
}
```

`POST` accepts `{body, quoted_message_id?, nonce?}`. Empty replies are rejected;
replying to a non-root message returns an error (`nested thread replies are not
supported`). `nonce` is an optional idempotency key; replaying the same nonce
with the same body and quote returns the existing reply with HTTP 200.

## Schema invariants

For any message:

- Root: `parent_message_id IS NULL`, `thread_root_id = id`, `channel_seq IS NOT NULL`,
  `thread_seq IS NULL`.
- Reply: `parent_message_id = root.id`, `thread_root_id = root.id`,
  `channel_seq IS NULL`, `thread_seq` assigned per-root.

## Thread state

`thread_state` is one row per root message, kept in sync inside the same
transaction as the reply insert. It carries:

- `reply_count`
- `last_reply_at`
- `last_reply_author_ids_json` — small ring of recent author IDs for "X, Y and 3
  others replied" UI.

Channel and DM message-page responses hydrate this state onto every root
message. The web timeline keeps a compact thread control visible, shows reply
count and recent activity when replies exist, and opens a thread from either
that control or non-interactive row content. Links, buttons, attachments, and
text selection keep their normal behavior.

A reply emits two durable events: `thread.reply_created` and
`thread.state_updated`. Both go into the workspace event stream and reach
subscribers via the realtime hub.

## Web thread lifecycle

The topbar thread control shows guidance above the composer when no thread is
selected. This guidance and recoverable action errors leave navigation active.

Selecting a thread clears the previous thread's replies immediately. Closing
or replacing the pane invalidates its pending loads, including route, search,
pin, and realtime refreshes. A delayed response cannot reopen a closed pane or
replace the thread selected afterward. Current background refresh failures
still stop realtime checkpointing until recovery.

Reply drafts and quotes belong to their root message for the current app
session. Switching threads or closing the pane preserves an unsent draft;
reopening that thread restores it. Submitting disables duplicate sends while
the request is pending. Failures show an error beside the reply composer and
retain the text and quote. Retrying unchanged content reuses the original
nonce, so a lost response does not create a duplicate reply. Drafts are kept in
memory and are not persisted across page reloads. Main and embedded thread
views keep the newest reply summary when HTTP receipts and realtime refreshes
arrive out of order.

## Ordering and pagination

Replies are always returned in ascending `thread_seq` order. This sequence is
local to the thread and is distinct from `channel_seq`. The default limit is
100; valid limits are 1–200 (out-of-range limits retain the legacy default of
100). With no cursor, clients receive the earliest replies; `latest=true`
selects the latest bounded window instead.

| Query | Window |
| --- | --- |
| `before_seq=S` | Nearest replies strictly before S |
| `after_seq=S` | Nearest replies strictly after S |
| `around_seq=S` | A balanced window including S, filling unused capacity from the other side |

Use one cursor at a time. Cursors must be nonnegative integers; combining them
or combining a cursor with `latest=true` returns HTTP 400. `latest=false`
behaves like omitted latest. Bounds and edge flags describe the returned reply
window, including deleted-message tombstones. An empty page has zero bounds
and false edge flags. Root hydration, the full thread summary, and current
channel/DM access checks apply to every mode.

Main and embedded views initially show the latest 100 replies. Load older/newer
controls fetch adjacent pages of 50 without moving the reading anchor. Ordinary
refresh revalidates the retained interval instead of replacing it with a new
100-reply slice. New replies follow only when already at the live edge; Jump to
latest and a successful own reply explicitly select and follow the latest
window. A newer history selection made while a reply is sending takes precedence
over its delayed receipt. Selecting a search reply supersedes pending parent
navigation, so returning to search and reopening a reply keeps its selection and
load errors visible. Search and quote jumps load around the actual reply, with a
visible error if it is unavailable. The root remains the canonical URL; a reload opens
latest rather than persisting a reply cursor.

The shared native thread panel retains at most 300 reply rows and trims toward
200, protecting the reading/editing anchor when possible. Trimmed edges remain
reloadable. This smaller budget is independent of the virtualized channel
window. Layout and media-size restoration run outside durable event ingestion,
so rendering cannot block realtime checkpoints. Every committed window also
reconciles the existing reaction and edit owners.

## What is intentionally missing

- Multi-level threads.
- Promoting a reply to a channel post.
- Following/unfollowing threads.
