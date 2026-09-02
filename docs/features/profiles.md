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
client can update a profile field, notifications, or appearance independently.
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
