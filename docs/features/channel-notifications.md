---
read_when:
  - changing channel notification preferences or mention delivery
---

# Channel notifications

Each channel member can choose one notification preference:

- `all` delivers alerts for every message and is the compatibility default.
- `mentions` delivers alerts only when the event's `mentioned_user_ids` includes the member.
- `muted` suppresses alerts.

The preference applies to Pushover, [web push](push-notifications.md), and browser or
desktop realtime alerts. Direct-message notifications are unchanged.

## Endpoints

```http
GET   /api/channels/{channel_id}/notification-settings
PATCH /api/channels/{channel_id}/notification-settings
```

The PATCH body is `{ "preference": "all" | "mentions" | "muted" }`. Both endpoints require
membership in the channel's workspace.

Message events expose `mentioned_user_ids` as a top-level array. Clients use that durable event
metadata when applying `mentions` mode, including for messages received while another channel is
selected.
