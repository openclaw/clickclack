---
title: Bot installs
description: How to create ClickClack bot identities, mint scoped tokens, and install those tokens into runtimes such as OpenClaw.
---

# Bot installs

A ClickClack bot install is three things:

1. A `kind=bot` user inside one workspace.
2. One scoped `ccb_...` bearer token for that bot.
3. A runtime config that stores the token and knows which workspace/channel to
   watch.

The token is the install secret. The bot user is the visible chat identity.
The runtime is whatever process uses the token: OpenClaw, CI, a small SDK
script, or a service daemon.

## Pick the bot shape

Use a **service bot** for shared automation that does not belong to one human:
deploy notifiers, triage agents, infrastructure bots, and shared OpenClaw
workers.

Use a **user-owned bot** when the bot should be visibly attached to a person:
personal OpenClaw agents, delegated assistants, or automation that should lose
access if the owner leaves the workspace.

Both are normal ClickClack users with `kind=bot`. A service bot has no
`owner_user_id`; a user-owned bot has `owner_user_id=<human user id>`. Bot
tokens always authenticate as the bot user, never as the owner.

## Service bots vs user-owned bots

Service bots are workspace assets. A workspace owner or moderator creates them,
rotates their tokens, revokes their tokens, and can remove them from the
workspace.

User-owned bots are personal automation identities. Only the human owner can
create the bot and rotate or revoke its tokens, even if that owner is only a
plain workspace member. Workspace owners and moderators can remove the bot from
a workspace, but they do not gain token rotation or revocation rights.

Any workspace member can inspect redacted token metadata such as the token name,
scopes, creation time, last-use time, and revocation state. Raw token values are
returned only once, when a token is created.

Removing either bot shape from a workspace deletes that workspace membership
and revokes that bot's tokens for that workspace. The bot user row stays so old
messages keep rendering and a user-owned bot can be installed elsewhere later.

## Create a service bot

Run this on the ClickClack host, against the same data directory as the server:

```sh
clickclack admin bot create \
  --data /var/lib/clickclack \
  --workspace wsp_... \
  --created-by usr_manager \
  --name "OpenClaw Service" \
  --handle openclaw-service \
  --scopes bot:write \
  --token-name openclaw-prod \
  --plain
```

Docker deployment:

```sh
docker exec clickclack clickclack admin bot create \
  --data /app/data \
  --workspace wsp_... \
  --created-by usr_manager \
  --name "OpenClaw Service" \
  --handle openclaw-service \
  --scopes bot:write \
  --token-name openclaw-prod \
  --plain
```

`--plain` prints only the raw `ccb_...` token. Capture it once, move it into
the target runtime secret store, and do not paste it into docs, tickets, chat,
or logs.

## Create a user-owned bot

Pass the human owner's user ID:

```sh
clickclack admin bot create \
  --data /var/lib/clickclack \
  --workspace wsp_... \
  --owner usr_peter \
  --created-by usr_peter \
  --name "Peter's OpenClaw" \
  --handle peter-openclaw \
  --scopes bot:write \
  --token-name openclaw-personal \
  --plain
```

The owner must be a human workspace member, and `--created-by` must match that
owner. A bot cannot own another bot. The server also checks that the owner is
still a workspace member when a user-owned bot token is used.

## Scopes

Start with the smallest useful bundle:

- `bot:read`: read workspace/channel/message/thread/DM state and realtime
  events.
- `bot:write`: `bot:read` plus posting messages, replies, DMs, and uploads.
- `bot:admin`: `bot:write` plus channel creation/update.

Use explicit comma-separated scopes when a runtime needs less than a bundle:

```sh
--scopes workspaces:read,channels:read,messages:read,realtime:read
```

Current MVP scopes are documented in [features/bots.md](features/bots.md).

## Install into OpenClaw

OpenClaw's ClickClack extension reads ClickClack accounts from
`channels.clickclack`. Keep tokens in env-backed secret refs:

```jsonc
{
  "channels": {
    "clickclack": {
      "enabled": true,
      "baseUrl": "https://app.clickclack.chat",
      "workspace": "clickclack",
      "defaultAccount": "service",
      "accounts": {
        "service": {
          "name": "OpenClaw Service",
          "token": { "source": "env", "provider": "default", "id": "CLICKCLACK_SERVICE_TOKEN" },
          "botUserId": "usr_...",
          "defaultTo": "channel:general",
          "allowFrom": ["*"]
        },
        "peter": {
          "name": "Peter's OpenClaw",
          "token": { "source": "env", "provider": "default", "id": "CLICKCLACK_PETER_TOKEN" },
          "botUserId": "usr_...",
          "defaultTo": "channel:general",
          "replyMode": "model",
          "model": "openai/gpt-5.4-mini",
          "senderIsOwner": true
        }
      }
    }
  }
}
```

Then set the environment on the OpenClaw process:

```sh
export CLICKCLACK_SERVICE_TOKEN=ccb_...
export CLICKCLACK_PETER_TOKEN=ccb_...
```

`workspace` may be the workspace ID (`wsp_...`) or slug. Targets are
`channel:<name-or-id>`, `thread:<message-id>`, or `dm:<user-id>`.

## Install into a small SDK bot

For a one-shot bot:

```sh
CLICKCLACK_URL=https://app.clickclack.chat \
CLICKCLACK_TOKEN=ccb_... \
CLICKCLACK_CHANNEL_ID=chn_... \
CLICKCLACK_TEXT="clack from bot" \
pnpm --filter @clickclack/example-bot start
```

For a long-running bot, use `ClickClackBot` from the TypeScript SDK. Persist
the latest event cursor after each handled event, reconnect with that cursor,
and ignore messages authored by the bot's own `botUserId`.

## Verify the install

Check identity:

```sh
curl -fsS \
  -H "Authorization: Bearer $CLICKCLACK_BOT_TOKEN" \
  https://app.clickclack.chat/api/me
```

Check workspace access:

```sh
curl -fsS \
  -H "Authorization: Bearer $CLICKCLACK_BOT_TOKEN" \
  https://app.clickclack.chat/api/workspaces
```

Post a smoke message with the SDK example or the OpenClaw extension. In the UI,
the message author should show the bot display name and a bot badge. A
user-owned bot profile should also show that it belongs to its owner.

## Rotate or remove

Rotate by creating a new workspace-scoped token, moving the runtime to the new
secret, then revoking the old token through
`POST /api/bot-tokens/{token_id}/revoke`.

Remove a bot from a workspace through
`DELETE /api/workspaces/{workspace_id}/bots/{bot_user_id}/membership`. This
revokes only that workspace's tokens for the bot. It does not delete the bot
identity, so old messages keep rendering.

Keep these rules:

- Never commit raw `ccb_...` tokens.
- Store production tokens in env files, 1Password, or the hosting provider's
  secret store.
- Use one token per runtime so rotation has a small blast radius.
- Use service bots for shared infrastructure and user-owned bots for delegated
  personal automation.
