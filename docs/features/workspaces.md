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
PATCH /api/workspaces/{workspace_id}           # manager: update name, slug, or icon
DELETE /api/workspaces/{workspace_id}          # owner: permanently delete workspace
POST /api/workspaces/{workspace_id}/transfer-ownership # owner: transfer to human member
GET  /api/workspaces/{workspace_id}/members   # paginated public member directory
```

`POST /api/workspaces` accepts `{name, slug?}`. Slugs default to a slugified
form of `name` and must be unique.

The owner who creates the workspace is auto-added with role `owner`. Adding
other members today goes through auth/bootstrap flows or admin commands; the
HTTP API exposes moderation for existing members, not arbitrary invites.

Owners and moderators can update the workspace name, slug, and icon. An icon
must reference an upload from the same workspace. Owners can transfer ownership
to a human member or moderator; the former owner becomes a moderator.

Workspace deletion is owner-only and permanent. The metadata transaction first
records every upload object in a durable cleanup queue, then deletes the
workspace and its dependent rows. A successful response means metadata deletion
committed. Object deletion is attempted immediately and any failure remains
queued for retry on the next server start.

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

Create body: `{name, display_title?, kind?, external_managed?, external_ref?,
external_url?, sidebar_section?}`. `name` is slugified to keep
`(workspace_id, name)` unique. `display_title` is an optional presentation-only
title; it is trimmed, limited to 200 Unicode characters, and does not affect
routing or uniqueness.
`kind` defaults to `public`. External management is opt-in and does not change
channel authorization: it records an opaque identity and optional deep link for
the application that owns the channel lifecycle.

`PATCH` accepts any subset of `{name, display_title, kind, archived,
external_managed, external_ref, external_url, sidebar_section}`. Setting
`archived=true` fills
`archived_at`; `archived=false` clears it. Sending an empty string for any of
the nullable display, external, or sidebar fields clears that field.

Channel responses include `display_title` when set. Human-facing web labels use
it and fall back to `name`; API selectors, links, and routing continue to use
the slug-like `name`.

Archived channels remain addressable and readable, but the web sidebar removes
them from the normal channel list and places them in a collapsed Archived group.
Non-archived channels with a non-empty `sidebar_section` appear in an
alphabetized labeled subgroup; channels without one keep the original flat-list
placement. Section and Archived disclosure state is browser-local and persisted
per workspace. `external_managed` adds a small row marker, while a safe HTTP(S)
`external_url` adds an external-open action to the channel header.

Guest workspace members are waiting-room users. They can only see `#guest`, can
post three messages per day, and cannot create rooms or DMs. Moderators and
owners can promote them to `member`, time them out, or block them. See
[moderation.md](moderation.md).

Channel write endpoints emit a durable `channel.created` or `channel.updated`
event into the workspace event stream so connected clients see the change
without polling. `channel.updated` includes the resulting `archived` boolean in
its payload so consumers can update visibility without refetching the channel.

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

Every channel root message receives an immutable `M...` route ID when it is
created. Its action menu can copy an absolute citation URL without changing
the current view. Opening that URL selects and highlights the root in its
channel; once the root has replies, the same URL also opens the thread panel.
Replies do not receive route IDs.

Existing DM thread URLs remain compatible, but the web app does not offer a
copy-link action for direct messages. All message URLs inherit the root
message's channel or DM visibility and grant no additional access.

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
