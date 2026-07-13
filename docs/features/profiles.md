---
read_when:
  - changing user profile fields, account settings UI, or /api/me
---

# Profiles

Each user has a display name, optional handle, optional avatar URL, and
per-user notification settings.

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

`PATCH /api/me` returns `{ "user": ... }`. Handles must be unique when set and
must be 2-32 characters using letters, numbers, `_`, or `-`. Avatar URLs can be
blank or an `http`/`https` URL.

Pushover notifications require `CLICKCLACK_PUSHOVER_API_TOKEN` on the server.
Each user opts in from account settings with their own 30-character Pushover
user key. Message-created pushes are delivered to opted-in workspace members
except the author; DMs are delivered only to opted-in conversation members
except the author.

## App

The current user's profile control sits at the bottom of the channel sidebar.
Click or right-click it to open account settings and edit display name, handle,
avatar URL, conversation display preferences, and notification settings.

Conversation display preferences can hide agent commentary or tool calls and
independently place the current user's messages and other human or agent
messages on the left or right. These preferences are stored on the local
device, not in the user profile returned by `/api/me`.

Clicking a message avatar or author name opens a Slack-style profile pane in
the right rail. The pane shows the user's avatar, display name, handle,
presence, user ID, and a Message action for starting or jumping to a DM.

Message lists, search results, threads, DMs, and the profile control all hydrate
avatars from the user attached to each message or conversation member.
