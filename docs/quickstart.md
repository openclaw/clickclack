---
title: Quickstart
description: From a fresh clone to a running ClickClack instance with a real owner, a workspace, a channel, and a bot — in about five minutes.
---

# Quickstart

The smallest useful ClickClack: a real owner account, one workspace, one
channel, a session token in your shell, a bot that posts a message. About five
minutes.

## 1. Boot the server

```sh
pnpm install
pnpm build
go run ./apps/api/cmd/clickclack serve
```

Open `http://localhost:8080` for the product website. Open
`http://localhost:8080/app` for the chat app with a `Local Captain` user
already signed in via the dev fallback. That's the empty-dev path — useful for
poking around, not what you want long-term.

## 2. Replace the dev user with a real owner

```sh
go run ./apps/api/cmd/clickclack admin bootstrap \
  --name "Peter" --email steipete@gmail.com
# prints usr_...
```

If the DB already has a user (the dev fallback created one),
`bootstrap` returns that user's ID instead of making a new one. Reset
by deleting `data/clickclack.db` if you want a clean slate.

## 3. Mint a session

Magic-link tokens are mintable from the CLI. Generate one and exchange it for
a session token:

```sh
TOKEN=$(go run ./apps/api/cmd/clickclack admin magic-link create \
  --email steipete@gmail.com --name "Peter")

SESSION=$(go run ./apps/api/cmd/clickclack login \
  --magic-token "$TOKEN" --plain --no-store)
# prints ses_...
```

That `ses_...` is what bots and CLIs put in `Authorization: Bearer`. The
browser already has it as the default `cc_session` cookie if you ran consume
from the SPA. Production deployments can select a stable namespaced cookie.

To store the session for future CLI commands, omit `--no-store`.

## 4. Make a workspace and a channel

You can use the SPA, or hit the API directly:

```sh
WORKSPACE=$(curl -s -X POST http://localhost:8080/api/workspaces \
  -H "authorization: Bearer $SESSION" \
  -H 'content-type: application/json' \
  -d '{"name":"Tide Pool"}' | jq -r .workspace.id)

CHANNEL=$(curl -s -X POST "http://localhost:8080/api/workspaces/$WORKSPACE/channels" \
  -H "authorization: Bearer $SESSION" \
  -H 'content-type: application/json' \
  -d '{"name":"general"}' | jq -r .channel.id)

echo "workspace=$WORKSPACE channel=$CHANNEL"
```

## 5. Post your first message

```sh
go run ./apps/api/cmd/clickclack \
  --server http://localhost:8080 \
  --token "$SESSION" \
  send --channel "$CHANNEL" "click. clack."

curl -s -X POST "http://localhost:8080/api/channels/$CHANNEL/messages" \
  -H "authorization: Bearer $SESSION" \
  -H 'content-type: application/json' \
  -d '{"body":"click. clack."}' | jq .message.body
```

Refresh the SPA — the message is there. Now hover it and click the thread
icon to start a thread; replies stream live over the WebSocket.

## 6. Run the bot example

```sh
CLICKCLACK_URL=http://localhost:8080 \
CLICKCLACK_TOKEN="$SESSION" \
CLICKCLACK_CHANNEL_ID="$CHANNEL" \
CLICKCLACK_TEXT="clack from a bot" \
pnpm --filter @clickclack/example-bot start
```

It posts a single message via the [TypeScript SDK](sdk.md). Use it as a
template — the SDK is framework-neutral, so the same code runs in Node, Bun,
Deno, browsers, or Cloudflare Workers.

## Where to go next

- [Architecture](architecture/overview.md) — how the parts fit.
- [Realtime](features/realtime.md) — cursor recovery and event types.
- [Auth](features/auth.md) — the four ways to identify a caller.
- [Deployment](deployment.md) — running this for real, not just on a laptop.
