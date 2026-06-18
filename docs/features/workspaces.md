---
read_when:
  - adding workspace or channel endpoints
  - changing membership, slugs, or channel kinds
---

# Workspaces & Channels

A workspace is the top-level container. It owns channels, direct conversations,
events, uploads, and invites. Membership lives in `workspace_members` with a
role of `owner`, `moderator`, `member`, `guest`, or `bot`.

## Workspaces

```http
GET  /api/workspaces                          # workspaces the caller belongs to
POST /api/workspaces                          # create + add caller as owner
GET  /api/workspaces/{workspace_id}           # one workspace, must be a member
GET  /api/workspaces/{workspace_id}/members   # paginated public member directory
```

`POST /api/workspaces` accepts `{name, slug?}`. Slugs default to a slugified
form of `name` and must be unique.

The owner who creates the workspace is auto-added with role `owner`. Adding
other members today goes through auth/bootstrap flows or admin commands; the
HTTP API exposes moderation for existing members, not arbitrary invites.

`GET /api/workspaces/{workspace_id}/members` is a read-only directory for any
workspace member. It accepts `limit` (default 100, max 200), opaque `cursor`,
case-insensitive literal `q` search over display name and handle, and optional
`role` (`owner`, `moderator`, `member`, `bot`, `guest`). It returns
`{members, next_cursor, has_more, total_count}` on the first page. Cursor pages
omit `total_count` so infinite scrolling does not repeat count work. The member
directory does not include moderation state.

## Channels

```http
GET  /api/workspaces/{workspace_id}/channels  # list, ordered by name
POST /api/workspaces/{workspace_id}/channels  # create
PATCH /api/channels/{channel_id}              # rename, change kind, archive
```

Create body: `{name, kind?}`. `name` is slugified to keep `(workspace_id, name)`
unique. `kind` defaults to `public`.

`PATCH` accepts any subset of `{name, kind, archived}`. Setting `archived=true`
fills `archived_at`; `archived=false` clears it.

Guest workspace members are waiting-room users. They can only see `#guest`, can
post three messages per day, and cannot create rooms or DMs. Moderators and
owners can promote them to `member`, time them out, or block them. See
[moderation.md](moderation.md).

Channel write endpoints emit a durable `channel.created` or `channel.updated`
event into the workspace event stream so connected clients see the change
without polling.

## Web routes

The web app uses public route IDs for conversation navigation:

```text
/app/{workspace_route_id}
/app/{workspace_route_id}/{target_route_id}
```

Route IDs are separate from the internal IDs used by API mutations and event
payloads. New copied links use `T...` for workspaces, `C...` for channels,
`D...` for direct conversations, and `M...` for thread root messages.

Old internal-ID links such as `/app/wsp_.../chn_...`, `/app/wsp_.../dm_...`,
and `/app/wsp_.../msg_...` remain compatibility inputs. The app resolves them
through `/api/routes/{workspace_route_id}/{target_route_id}` and replaces the
URL with the canonical public route after permission checks.

Thread URLs resolve through the root message, inherit that message's channel or
DM visibility, and then open the thread panel in the parent conversation.

When a user opens a bare workspace route, the web app returns to the last
channel that browser visited in that workspace. If that saved channel is no
longer visible, the app falls back to the first listed channel, then to the
first direct conversation.

## Membership rules

- Every workspace mutation checks `requireMembership(workspace_id, user_id)`.
- Listing channels, sending messages, opening threads, posting reactions,
  uploading files, and subscribing over WebSocket all go through the same
  check.
- Channel listing returns archived channels too — the UI is expected to
  render them differently. Filter on the client if you want only active
  channels.

## What is intentionally missing

- Private channels with explicit member sets (planned but not modeled in V1).
- Arbitrary HTTP member invites/additions.
- Channel topic, description, or pinned messages.
