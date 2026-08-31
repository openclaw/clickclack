---
read_when:
  - changing channel pins or pinned-message events
---

# Pinned messages

Channel members can pin up to 100 non-deleted messages in a channel. The pinned panel always shows
the whole channel's pins, even while the timeline is filtered to a topic. It preserves topic badges
and resolved-mention rendering so a pinned message reads the same as it does in the timeline. Pins
are shared channel state, not a per-user bookmark. Direct messages do not support pins.

Pin refreshes belong to the selected channel. A delayed pin or unpin response
from a previous channel cannot replace the current channel's pin state.

## Endpoints

```http
GET    /api/channels/{channel_id}/pins
POST   /api/channels/{channel_id}/pins
DELETE /api/channels/{channel_id}/pins/{message_id}
```

The POST body is `{ "message_id": "msg_..." }`. Listing returns full message objects newest pin
first. A duplicate pin or a channel at its 100-message limit returns HTTP 409; the app also shows
the current count against that ceiling in the panel header.

## Events

- `pin.added` after a message is pinned
- `pin.removed` after a message is unpinned

Both events carry `channel_id`, `message_id`, and `pinned_by` in their payload so connected clients
can refresh the shared panel.
