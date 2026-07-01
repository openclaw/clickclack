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
  "thread_state":  ThreadState         // counters/last reply summary
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

## Ordering and pagination

Replies are ordered by `thread_seq` ascending. `limit` is clamped to `1..200`
(default 100). There's no `after_seq` parameter on the thread endpoint yet —
clients fetch the head of the thread and use realtime events for new replies.

## What is intentionally missing

- Multi-level threads.
- Promoting a reply to a channel post.
- Following/unfollowing threads.
