---
read_when:
  - changing direct message conversations or DM listing
---

# Direct Messages

DMs are workspace-scoped multi-party conversations. They reuse the `messages`
table — every DM message sets `direct_conversation_id` and leaves
`channel_id` null.

## Endpoints

```http
GET  /api/dms?workspace_id=                              # caller's conversations in a workspace
POST /api/dms                                            # { workspace_id, member_ids }
GET  /api/dms/{conversation_id}                          # direct access, including closed DMs
DELETE /api/dms/{conversation_id}                        # close for the current human user
POST /api/dms/{conversation_id}/open                     # reopen for the current human user
GET  /api/dms/{conversation_id}/messages?after_seq=&limit=
POST /api/dms/{conversation_id}/messages                 # { body, quoted_message_id?, nonce? }
POST /api/dms/{conversation_id}/read                     # { seq }
```

Conversations include their members hydrated from `users`. The `member_ids`
list on create is deduplicated and the caller is added automatically.

The web sidebar lists existing DMs and also derives a People section from DM
members and hydrated message authors. Users appear there automatically as
conversation context is loaded; clicking a person opens their DM when one
exists, otherwise it opens the profile pane with a Message action.

Closing a DM only hides it from the current user's sidebar. Membership,
history, routes, and read state remain intact for every member. Direct links
still resolve. Reopening the same one-to-one member set, using the explicit
open endpoint, or receiving a new root message makes the conversation visible
again. The web sidebar exposes Close and an eight-second Undo action. Bot
tokens cannot close or reopen a human user's sidebar state.

`POST` to `/dms/{id}/messages` increments a per-conversation sequence on
`messages.channel_seq` and emits a durable private event into the workspace
event stream so DM lists and unread counts stay live for conversation members.
`nonce` has the same retry-safe idempotency behavior as channel message
creation.

`POST /dms/{id}/read` updates the caller's monotonic read pointer for that
conversation and emits a private `dm.read` event only to the caller's own
sessions.

## Membership

- Listing conversations requires workspace membership.
- Sending a DM requires membership in the conversation
  (`direct_conversation_members`).
- DM creation requires that all `member_ids` are members of the same
  workspace.
- Editing, deleting, reacting to, attaching uploads to, or opening threads on
  a DM message requires direct conversation membership. Edits and deletes are
  author-only.

## Threads

DM root messages support the same one-level thread model as channel messages.
Thread replies carry `direct_conversation_id`, use `thread_seq`, and do not
appear in the root DM timeline or unread root-message sequence.

## What is intentionally missing

- DM-only auth tokens.
- One-on-one vs group distinctions in the API surface — the client decides
  based on member count.
- DM-only search. Workspace/channel search excludes DMs; DM search needs a
  separate endpoint so private messages never leak into channel results.
