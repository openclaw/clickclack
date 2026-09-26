---
read_when:
  - changing user profile fields, account settings UI, or /api/me
---

# Profiles

Each user has a display name, optional handle, optional avatar URL, and
per-user notification settings. Email-backed users without an explicit or
provider-supplied avatar use a Gravatar generated from their normalized email.

The handle is the human-friendly short name shown as `@name` in the app. The
API accepts it with or without the leading `@`, normalizes it to lowercase, and
stores it without the `@`.

## API

```http
GET /api/me

PATCH /api/me
{
  "display_name": "Peter Steinberger",
  "handle": "@steipete",
  "avatar_url": "https://example.com/avatar.png",
  "notification_settings": {
    "pushover_enabled": true,
    "pushover_user_key": "uQiRzpo4DXghDmr9QzzfQu27cmVRsG"
  }
}
```

`PATCH /api/me` returns `{ "user": ... }`. Omitted fields are unchanged, so a
client can update a profile field, notifications, appearance, or sidebar
preferences independently.
Handles must be unique when set and must be 2-32 characters using letters,
numbers, `_`, or `-`. Avatar URLs can be
blank or an `http`/`https` URL. An explicit URL takes precedence over Gravatar;
clearing it restores the email-backed Gravatar fallback. Gravatar requests are
served by `gravatar.com`, so clients loading those images contact that external
service.

Pushover notifications require `CLICKCLACK_PUSHOVER_API_TOKEN` on the server.
Each user opts in from account settings with their own 30-character Pushover
user key. Message-created pushes are delivered to opted-in workspace members
except the author; DMs are delivered only to opted-in conversation members
except the author.

## App

The current user's profile control sits at the bottom of the channel sidebar.
Click or right-click it to open account settings and edit display name, handle,
avatar URL, conversation display preferences, and notification settings.
The account settings rail shows one Workspace group. When the account belongs
to more than one workspace, a selector under the heading chooses which one the
group's sections apply to; it opens on the workspace the user is standing in,
whose name carries a "(current)" marker in the list, so a workspace section
opens that workspace rather than whichever one the API listed first. With a
single workspace the heading names it and there is no selector. Opening account
settings on a phone closes the navigation drawer.

Profile and notification saves update only their respective sections. Saving
one section preserves changes to another section made in another tab or device.
Fields stay disabled while their save is pending. Leaving a section or closing
account settings prevents its delayed response from replacing a newer draft,
reverting appearance, or closing another dialog. Each section refreshes when
opened, so saves already accepted by the server remain visible when returning.

Conversation display preferences can hide agent commentary or tool calls and
independently place the current user's messages and other human or agent
messages on the left or right. These preferences are stored on the local
device, not in the user profile returned by `/api/me`.

The personal sidebar channel order does roam with the account. `GET /api/me`
reports it as `sidebar_preferences.channel_order`, an object keyed by workspace
id whose values are ordered channel ids, and `PATCH /api/me` accepts the same
shape:

```http
PATCH /api/me
{
  "sidebar_preferences": {
    "channel_order": { "wsp_01hzy": ["chn_01hzz", "chn_01j00"] }
  }
}
```

A workspace present in the patch replaces that workspace's order, omitted
workspaces are unchanged, and an empty array clears one workspace back to the
server's default ordering. The caller must be a member of every workspace
listed, or the request is rejected with `403`. Ids that are not channels of
that workspace are dropped rather than rejected, so a channel deleted since the
sidebar rendered cannot block a save, and a repeated id keeps its first
position. One workspace holds at most 500 ids.

A cleared workspace stays in later `GET /api/me` responses with an empty array
rather than disappearing, so a browser holding a cached order can tell a clear
from a workspace that never saved one and drop its copy instead of restoring
it. The row goes away only when the membership it hangs off does.

Reordering still applies on the device first and localStorage stays the
pre-paint cache, so drag, keyboard, and touch moves take effect without waiting
on the network and survive a server that cannot be reached. The account order
wins on load and is written back into the cache. Because the account copy stops
at 500 ids, it leads the cached order rather than replacing it: local positions
past that cap stay on the device that made them. The account write is debounced
and best effort: a failed write leaves the local order in place and the next
reorder retries. Writes for one workspace are serialized, and an order that
arrives while a write is in flight replaces any other waiting order, so the
newest order is the one that lands. An account snapshot is applied once per
loaded profile and workspace, so moving between workspaces re-reads the cache
instead of replaying a snapshot over an order another tab has since saved.

Clicking a message avatar or author name opens a Slack-style profile pane in
the right rail. The pane shows the user's avatar, display name, handle,
presence, user ID, and a Message action for starting or jumping to a DM.
Opening or closing a profile keeps the URL on the visible conversation and
supersedes an older navigation that is still loading. Opening a profile from
a thread returns to that thread's parent conversation.

Message lists, search results, threads, DMs, and the profile control all hydrate
avatars from the user attached to each message or conversation member.
The member directory uses the same avatar fallback: an unavailable image shows
the user's initial while retaining the member or bot styling.
