---
read_when:
  - changing reaction add/remove or the reactions table
---

# Reactions

Emoji reactions are a `(message_id, user_id, emoji)` triple. One reaction per
user per emoji per message.

## Endpoints

```http
POST   /api/messages/{message_id}/reactions
DELETE /api/messages/{message_id}/reactions/{emoji}
```

`POST` body: `{emoji}`. Both endpoints require workspace membership for the
message's workspace. Adding twice is a no-op that returns HTTP 200 without an
event; removing a missing reaction is a no-op.

Message reads expose reactions as bounded per-emoji summaries:

```json
{"emoji":"lobster","count":3,"reacted_by_me":true}
```

The API does not include the individual reacting users in message payloads.

## Events

- `reaction.added` on add
- `reaction.removed` on remove

The event payload contains `{message_id, emoji}` and inherits the message's
`channel_seq` so that reaction counters can be ordered alongside the message
they decorate.

## Storage

Reactions are stored verbatim — there's no allowlist or canonical shortcode
table. Pass any string the UI is willing to render. Crustacean reactions like
`:lobster:` and `:claw:` are intended in the reaction pack but not enforced
server-side.
