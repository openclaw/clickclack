---
read_when:
  - changing REST endpoints, websocket behavior, SDK methods, or OpenAPI
  - adding integrations or bots
---

# API Overview

`packages/protocol/openapi.yaml` is the API contract source of truth. Server
handlers live in `apps/api/internal/httpapi`; the TypeScript SDK in
`packages/sdk-ts` is the typed client.

## Auth

The server resolves callers in this order (see
[../features/auth.md](../features/auth.md)):

1. `Authorization: Bearer <token>` (session token, magic-link result).
2. `cc_session` cookie.
3. `X-ClickClack-User: usr_...` header (local/dev impersonation for tests).
4. Dev fallback to the first user in the DB (only with
   `--dev-bootstrap=true`).

## Endpoint groups

| Group         | Endpoints | Doc |
|---------------|-----------|-----|
| Auth          | `/api/auth/magic/{request,consume}`, `/api/auth/github/{start,callback}` | [auth](../features/auth.md) |
| Profile       | `/api/me` | [profiles](../features/profiles.md) |
| Workspaces    | `/api/workspaces`, `/api/workspaces/{id}` | [workspaces](../features/workspaces.md) |
| Moderation    | `/api/workspaces/{id}/moderation/members` | [moderation](../features/moderation.md) |
| Bots          | `/api/workspaces/{id}/bots`, `/api/bots/{id}/tokens`, `/api/bot-tokens/{id}/revoke` | [bots](../features/bots.md) |
| App installs  | `/api/workspaces/{id}/app-installations`, `/api/app-installations/{id}/revoke` | [integrations](../features/integrations.md) |
| Slash commands | `/api/workspaces/{id}/slash-commands`, `/api/slash-commands/{id}/revoke`, `/api/hooks/slash/{channel}` | [integrations](../features/integrations.md) |
| Event subscriptions | `/api/workspaces/{id}/event-subscriptions`, `/api/event-subscriptions/{id}/{revoke,deliveries}` | [integrations](../features/integrations.md) |
| Audit log     | `/api/workspaces/{id}/audit-log` | [integrations](../features/integrations.md) |
| Connected accounts | `/api/workspaces/{id}/connected-accounts`, `/api/connected-accounts/{id}/revoke` | [integrations](../features/integrations.md) |
| Topics        | `/api/workspaces/{id}/topics` | [messages](../features/messages.md) |
| Channels      | `/api/workspaces/{id}/channels`, `/api/channels/{id}` | [workspaces](../features/workspaces.md) |
| Messages      | `/api/channels/{id}/messages`, `/api/messages/{id}` | [messages](../features/messages.md) |
| Threads       | `/api/messages/{id}/thread`, `/api/messages/{id}/thread/replies` | [threads](../features/threads.md) |
| Replies       | `quoted_message_id` on any message-create endpoint | [replies](../features/replies.md) |
| Reactions     | `/api/messages/{id}/reactions` | [reactions](../features/reactions.md) |
| Realtime      | `/api/realtime/{ws,events,ephemeral}` | [realtime](../features/realtime.md) |
| Search        | `/api/search` | [search](../features/search.md) |
| Uploads       | `/api/uploads`, `/api/messages/{id}/attachments` | [uploads](../features/uploads.md) |
| DMs           | `/api/dms`, `/api/dms/{id}`, `/api/dms/{id}/open`, `/api/dms/{id}/messages` | [dms](../features/dms.md) |
| Integrations  | `/api/hooks/mattermost/{channel}` | [integrations](../features/integrations.md) |

## Conventions

- All payloads are JSON unless explicitly multipart (uploads) or
  form-encoded (slash commands).
- Mutating endpoints return both the affected resource and the durable event
  emitted (`{message, event}`), so clients can reconcile optimistically.
- Pagination on listings uses cursor-style sequence numbers (`after_seq` for
  channel/DM messages, `after_cursor` for events).
- Errors come back as `{ "error": "<message>" }` with an HTTP status code.
- Store-level moderation restrictions surface as `403`; exhausted guest post
  budgets surface as `429`.

## SDK

TypeScript consumers should use `@clickclack/sdk-ts`. It depends on neither
Svelte nor any HTTP framework. See [../sdk.md](../sdk.md).
