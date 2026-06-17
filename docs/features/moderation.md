---
read_when:
  - changing guest-room admission, member roles, timeouts, blocks, or moderator UI
  - touching workspace member moderation endpoints
---

# Moderation

ClickClack has a small waiting-room model for public GitHub sign-in. It is
workspace-scoped and implemented in the store layer so HTTP handlers,
realtime replay, uploads, DMs, and push notifications all share the same
visibility rules.

## Roles

Workspace membership roles:

- `owner` - creator/admin of a workspace. Owners cannot be moderated through
  the member moderation API.
- `moderator` - can view the moderation roster and moderate lower-ranked
  members.
- `member` - normal room participant.
- `guest` - waiting-room participant with restricted access.
- `bot` - service identity used by bot tokens.

Role rank is enforced. Moderators cannot moderate themselves, owners, or
another owner/moderator at the same or higher rank. The moderation endpoint
also refuses to assign `owner`; owner creation stays with workspace creation.

## Guest Workspace

Open GitHub login without `CLICKCLACK_GITHUB_ALLOWED_ORG` joins users to the
isolated `Guests` workspace. The store ensures two public channels exist:
`#guest` and `#general`.

When `CLICKCLACK_GITHUB_MODERATOR_ORG` is set:

- GitHub users who are members of that org become `moderator` in `Guests`.
- Everyone else starts as `guest`.
- Guests only see and post in `#guest`.
- Guests have a rolling three-post budget per 24 hours.
- Moderators can promote a guest to `member`; members are not post-limited and
  can use the normal workspace rooms.

When `CLICKCLACK_GITHUB_MODERATOR_ORG` is unset, open-login users join
`Guests` as `member` so the workspace does not become an unattended room.

## Guest Restrictions

Guests are deliberately narrow:

- `GET /api/workspaces/{workspace_id}/channels` only returns `#guest`.
- Posting outside `#guest` returns a moderation error.
- The fourth guest post in a rolling 24-hour window returns `429`.
- Guests cannot create channels, rename channels, create DMs, send DMs,
  upload files, attach files, search hidden rooms, resolve hidden routes, or
  receive hidden realtime events.
- Posts made before a demotion to `guest` are not counted against the guest
  budget. Guest posts that are later deleted still count.

## Timeout And Block

Timeouts and blocks live in `workspace_member_moderation`.

- `timeout_until` blocks writes until the timestamp passes.
- `blocked_at` blocks writes until a moderator clears it.
- `moderation_note`, `moderation_by`, and `moderation_at` retain the current
  moderation note and attribution.

A timed-out or blocked user cannot post messages or replies, edit/delete
messages, react, upload or attach files, create or change channels, create or
send DMs, or moderate other users. Reads that the user could already perform
remain readable unless the user's role also hides the target.

## API

Moderators and owners use:

```http
GET /api/workspaces/{workspace_id}/moderation/members
PATCH /api/workspaces/{workspace_id}/moderation/members/{user_id}
```

The separate public member directory is
`GET /api/workspaces/{workspace_id}/members`; it is paginated and deliberately
does not include moderation state.

`GET` returns `members`, each with the user, role, moderation state, and guest
post budget:

```jsonc
{
  "members": [
    {
      "workspace_id": "wsp_...",
      "user": { "id": "usr_...", "display_name": "A Guest" },
      "role": "guest",
      "posts_remaining": 2,
      "post_limit": 3,
      "timeout_until": "2026-05-17T11:00:00Z",
      "blocked_at": null,
      "moderation_note": "cooling off",
      "moderation_by": "usr_mod",
      "moderation_at": "2026-05-17T10:00:00Z"
    }
  ]
}
```

`PATCH` accepts any subset of:

```jsonc
{
  "role": "member",
  "timeout_until": "2026-05-17T11:00:00Z",
  "timeout_minutes": 60,
  "clear_timeout": true,
  "blocked": false,
  "moderation_note": "approved"
}
```

Responses include the updated `member` and the durable
`member.moderation_updated` event. That event is private to the target user
and current owners/moderators, so clients can refresh visible moderation state
without leaking roster changes to the room.

## UI

Owners and moderators see moderation controls from the profile side pane:

- approve/promote a guest to `member`
- timeout for a short duration
- block or unblock

The client refreshes moderation state after `member.moderation_updated`. If
the current user is demoted or blocked, the side pane is cleared so stale
controls do not remain active.

## What is intentionally missing

- Global moderation across workspaces.
- Audit-log history beyond the current moderation row and event stream.
- Channel-specific roles or private-channel ACLs.
