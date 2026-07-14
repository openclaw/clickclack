---
read_when:
  - touching webhook handlers or slash command callbacks
  - planning Mattermost-compatible behavior
---

# Integrations

ClickClack ships two integration surfaces designed to look like a small subset
of Mattermost so existing scripts can post messages without rewriting.

## App installations

An app installation binds a named integration to a workspace and to the bot user
that will act for it. It is workspace integration metadata, not a runtime plugin
loader.

```http
POST /api/workspaces/{workspace_id}/app-installations
Content-Type: application/json

{
  "app_slug": "openclaw",
  "display_name": "OpenClaw",
  "bot_user_id": "usr_...",
  "config": {
    "default_channel_id": "chn_..."
  }
}
```

Behavior:

- Any human workspace member can list installations.
- Creating or revoking requires a human workspace owner or moderator. Bot
  tokens cannot mutate app installations.
- `bot_user_id` must be a bot member of the same workspace.
- `GET /api/workspaces/{workspace_id}/app-installations` lists active
  installations.
- `POST /api/app-installations/{installation_id}/revoke` atomically revokes the
  binding and, by default, its attached slash commands and event subscriptions.
- Its optional body fields are `revoke_slash_commands` (default `true`),
  `revoke_event_subscriptions` (default `true`), and `revoke_bot_tokens`
  (default `false`).
- Bot tokens for the installation's bot remain active unless
  `revoke_bot_tokens` is explicitly `true`, because the bot may serve other
  integrations.
- The revoke response contains the installation plus exact counts for revoked
  slash commands, event subscriptions, and bot tokens. Repeating the revoke
  returns zero counts.

The TypeScript SDK exposes this as `client.apps.list(workspaceId)`,
`client.apps.install(workspaceId, input)`, and
`client.apps.revoke(id, options)`.

## Incoming webhook

```http
POST /api/hooks/mattermost/{channel_id}
Content-Type: application/json
{ "text": "deploy ✅" }
```

Behavior:

- Authenticates the caller like any other API request. Use bearer session
  tokens for real integrations; reserve `X-ClickClack-User` for local/dev
  setups.
- Posts the `text` field as a Markdown channel message authored by the
  current user.
- Emits a `message.created` durable event.

Mattermost's full incoming webhook schema (`username`, `icon_emoji`,
`attachments`, etc.) is not honored — only `text`. Field names match so
existing senders don't crash.

## Registered slash commands

Use registered slash commands when an app owns the command and needs a signed
callback:

```http
POST /api/workspaces/{workspace_id}/slash-commands
Content-Type: application/json

{
  "app_installation_id": "app_...",
  "command": "/deploy",
  "description": "Deploy an environment",
  "callback_url": "https://example.com/clickclack/deploy",
  "bot_user_id": "usr_..."
}
```

Behavior:

- Any human workspace member can list slash commands.
- Creating, revoking, or rotating a secret requires a human workspace owner or
  moderator. Bot tokens cannot mutate slash command registrations.
- `bot_user_id` must be a bot member of the same workspace.
- `command` is normalized to a lowercase leading-slash command such as
  `/deploy`.
- `POST /api/workspaces/{workspace_id}/slash-commands` returns a one-time
  `signing_secret`; list calls redact it.
- `POST /api/slash-commands/{command_id}/rotate-secret` updates the
  registration in place and returns the fresh one-time `signing_secret`.
  Rotating a revoked command returns `400`.
- Invoking the command through `/api/hooks/slash/{channel_id}` sends JSON to
  `callback_url` with `X-ClickClack-Timestamp` and
  `X-ClickClack-Signature: sha256=<hex hmac>`, where the signed string is
  `<timestamp>.<raw-json-body>`.
- If the callback returns `{"response_type":"in_channel","text":"..."}`, the
  server posts that text as the command's bot user. `response_type` defaults to
  `in_channel`.

The callback payload includes `command_id`, `command`, `text`, `workspace_id`,
`channel_id`, `user_id`, `bot_user_id`, and `trigger_id`. Each invocation is
stored with callback status/body/error metadata for later audit.

The TypeScript SDK exposes this as `client.slashCommands.list(workspaceId)`,
`client.slashCommands.create(workspaceId, input)`,
`client.slashCommands.revoke(id)`, and
`client.slashCommands.rotateSecret(id)`.

In the web app, channel composers discover registered commands inline: type
`/` at the start of a draft to open the slash-command menu, keep typing to
filter by command name, then use the arrow keys plus `Enter` or `Tab` to insert
the selected command. The menu overlays the timeline instead of resizing the
message list.

The same composer menu supports `@` mentions for workspace people and bots.
Mention suggestions work in channel, DM, and thread composers; selection uses
the same mouse, arrow-key, `Enter`, and `Tab` controls as slash commands.

## Outgoing event subscriptions

Outgoing event subscriptions push durable event-log entries to app callbacks.
They are intentionally built on the same event records used by realtime
WebSocket replay.

```http
POST /api/workspaces/{workspace_id}/event-subscriptions
Content-Type: application/json

{
  "app_installation_id": "app_...",
  "event_types": ["message.created", "reaction.added"],
  "callback_url": "https://example.com/clickclack/events"
}
```

Behavior:

- Any human workspace member can list subscriptions and delivery attempts.
- Creating, revoking, or rotating a secret requires a human workspace owner or
  moderator. Bot tokens cannot mutate subscriptions.
- `event_types` accepts exact durable event types or `*`; unknown event types
  return `400`.
- `GET /api/event-types` returns the accepted durable event vocabulary to any
  authenticated user.
- Creation returns a one-time `signing_secret`; list calls redact it.
- `POST /api/event-subscriptions/{subscription_id}/rotate-secret` updates the
  subscription in place and returns the fresh one-time `signing_secret`.
  Existing delivery history remains attached. Rotating a revoked subscription
  returns `400`.
- Delivery posts `{"subscription_id":"sub_...","event": Event}` as JSON to the
  callback URL.
- Delivery uses the same signature headers as slash commands, plus
  `X-ClickClack-Event-ID`.
- Every delivery attempt is stored with response status, response body, error,
  and attempt number.
- `GET /api/event-subscriptions/{subscription_id}/deliveries` accepts `limit`
  (default `50`, maximum `200`) and a `before` delivery-attempt ID. Results are
  newest first and return `{"deliveries":[...],"next_cursor":"eda_..."}`.
  `next_cursor` is `null` on the last page.

The TypeScript SDK exposes this as
`client.eventSubscriptions.list(workspaceId)`,
`client.eventSubscriptions.create(workspaceId, input)`,
`client.eventSubscriptions.revoke(id)`, and
`client.eventSubscriptions.rotateSecret(id)`.
Use `client.eventSubscriptions.deliveries(id, { limit, before })` for delivery
pages and `client.eventTypes.list()` for the durable event vocabulary.

## Connected accounts and audit log

Connected accounts record external identities or app-side account bindings for a
workspace user:

```http
POST /api/workspaces/{workspace_id}/connected-accounts
Content-Type: application/json

{
  "user_id": "usr_...",
  "provider": "github",
  "provider_account_id": "123456",
  "display_name": "octocat",
  "scopes": ["repo:read"],
  "metadata": {"login": "octocat"}
}
```

Behavior:

- Any human workspace member can list connected accounts.
- Creating a connected account requires a human workspace owner or moderator.
- The target `user_id` must be a member of the workspace.
- `GET /api/workspaces/{workspace_id}/connected-accounts` lists active
  connected accounts.
- `POST /api/connected-accounts/{account_id}/revoke` revokes the account
  binding. A workspace owner or moderator can revoke any binding; the bound
  user can revoke their own.
- Create and revoke write audit entries.

Audit entries are available from `GET /api/workspaces/{workspace_id}/audit-log`.
The SDK exposes these as `client.connectedAccounts.*` and
`client.auditLog.list(workspaceId)`.

## Compatibility slash command callback

```http
POST /api/hooks/slash/{channel_id}
Content-Type: application/x-www-form-urlencoded
command=/deploy&text=staging
```

Behavior:

- Posts `<command> <text>` (trimmed) as a channel message authored by the
  caller.
- Returns a Mattermost-shaped response:

```jsonc
{
  "response_type": "in_channel",
  "text":          "<command> <text>",
  "message":       Message,
  "event":         Event
}
```

If the posted `command` is not registered, ClickClack falls back to the legacy
Mattermost-shaped behavior above and posts `<command> <text>` directly as the
caller. This keeps simple scripts working while registered commands handle real
app callbacks.

## What is intentionally missing

- Retry scheduling for failed outgoing deliveries.
- Mattermost client compatibility (REST, WebSocket protocol).
