---
read_when:
  - changing the TypeScript SDK or the OpenAPI shape
  - adding or maintaining bot examples
---

# TypeScript SDK

`@clickclack/sdk-ts` is the framework-neutral client. It wraps the HTTP API
and the realtime WebSocket without any Svelte dependency, so bots, CLIs, and
non-Svelte frontends can use it directly.

Source: `packages/sdk-ts/src/index.ts`.

## Install (workspace)

The SDK is published from the monorepo. Inside this repo, depend on it as
`@clickclack/sdk-ts` via pnpm workspaces. External consumers will install it
once it's published.

## Quick start

```ts
import { ClickClackClient } from "@clickclack/sdk-ts";

const client = new ClickClackClient({
  baseUrl: "http://localhost:8080",
  token: process.env.CLICKCLACK_TOKEN,        // session token or ccb_ bot token
  userId: process.env.CLICKCLACK_USER_ID,     // optional local/dev override
});

const me = await client.me();
await client.updateMe({ display_name: "Peter Steinberger", handle: "@steipete" });
const workspaces = await client.workspaces.list();
const channels = await client.channels.list(workspaces[0].id);
const message = await client.channels.sendMessage(channels[0].id, {
  body: "click clack",
  nonce: crypto.randomUUID(),
});
await client.channels.markRead(channels[0].id, message.channel_seq ?? 0);
```

## Auth

The client sends, in this order:

- `Authorization: Bearer <token>` if `token` was set or `auth.consumeMagicLink`
  succeeded (it stores the returned session token).
- `X-ClickClack-User: <userId>` if `userId` was set. Use this only for local
  development/test impersonation; hosted bots should use bearer tokens.

Helpers:

```ts
client.auth.requestMagicLink({ email });          // POST /api/auth/magic/request
client.auth.consumeMagicLink(token);              // POST /api/auth/magic/consume; sets bearer
client.auth.setToken(token);                      // store an externally-issued token
client.auth.githubStartUrl();                     // build the OAuth start URL for the browser
```

See [features/auth.md](features/auth.md).

## Surface

| Group         | Methods |
|---------------|---------|
| `me()`, `updateMe()` | get or edit the current user's profile |
| `workspaces`  | `list`, `create`, `get`, `update`, `transferOwnership`, `delete` |
| `topics`      | `list`, `create` |
| `bots`        | `listMine`, `list`, `create`, `removeMembership`, `listWorkspaceTokens`, `createWorkspaceToken`, `listTokens`, `createToken`, `revokeToken` |
| `apps`        | `list`, `install`, `revoke(id, options?)` |
| `slashCommands` | `list`, `create`, `revoke`, `rotateSecret` |
| `eventSubscriptions` | `list`, `create`, `revoke`, `rotateSecret`, `deliveries(id, options?)` |
| `eventTypes`  | `list` |
| `auditLog`     | `list` |
| `connectedAccounts` | `list`, `create`, `revoke` |
| `channels`    | `list`, `create`, `update`, `messages`, `sendMessage`, `markRead` |
| `messages`    | `get`, `findByNonce(workspaceId, nonce)`, `update`, `delete` |
| `threads`     | `get`, `reply` |
| `search(workspaceId, q)` | full-text search |
| `uploads`     | `create(workspaceId, file, filename?, { nonce? })`, `findByNonce(workspaceId, nonce)`, `attach(messageId, uploadId)` |
| `dms`         | `list`, `create`, `get`, `close`, `open`, `messages`, `sendMessage`, `markRead` |
| `events`      | `publishEphemeral`, `subscribe` |

## Realtime subscription

```ts
const socket = client.events.subscribe({
  workspaceId,
  afterCursor: lastSeenCursor,
  onEvent(event) {
    // event.type, event.payload, event.cursor
  },
  onClose() {
    // backoff and resubscribe with the latest cursor
  },
});
```

`subscribe` returns a raw `WebSocket`. Call `.close()` to disconnect. See
[features/realtime.md](features/realtime.md) for cursor recovery rules.

## Generated types

`packages/sdk-ts/src/generated/openapi.d.ts` is generated from
`packages/protocol/openapi.yaml`. Re-export at the top of `index.ts`:

```ts
export type { components, paths } from "./generated/openapi";
```

Use the friendly hand-written types (`User`, `Workspace`, `Message`, etc.)
for app code; reach into `components["schemas"]` only when you need the
exact OpenAPI shape.

## Bot accounts

Hosted bots should use bot tokens, not human session tokens. Create one from
the admin CLI:

```sh
clickclack admin bot create \
  --workspace wsp_... \
  --created-by usr_manager \
  --name "OpenClaw Service" \
  --handle openclaw \
  --scopes bot:write \
  --plain
```

The returned `ccb_...` token goes into `CLICKCLACK_TOKEN`.

Human-session clients can also manage bot lifecycle through the SDK:

```ts
const { bot, bot_token } = await client.bots.create(workspaceId, {
  display_name: "OpenClaw Service",
  handle: "openclaw",
  token_name: "prod",
  scopes: ["bot:write"],
});

const tokens = await client.bots.listWorkspaceTokens(workspaceId, bot.id);
await client.bots.revokeToken(tokens[0].id);
```

Use `createWorkspaceToken(workspaceId, bot.id, input)` for rotation. The older
`listTokens` and `createToken` helpers call the legacy bot-only routes and only
work for bots installed in exactly one workspace.

Only `create`, `createToken`, and `createWorkspaceToken` responses include the
one-time raw `bot_token.token`. List calls return metadata only.

The SDK also exports `ClickClackBot`, a tiny runner around the same client plus
the realtime WebSocket:

```ts
import { ClickClackBot } from "@clickclack/sdk-ts";

const bot = new ClickClackBot({
  baseUrl: "http://localhost:8080",
  token: process.env.CLICKCLACK_TOKEN,
  workspaceId: process.env.CLICKCLACK_WORKSPACE_ID!,
  onEvent(event, client) {
    if (event.type !== "message.created") return;
    const channelId = event.channel_id;
    if (channelId) void client.channels.sendMessage(channelId, { body: "ack" });
  },
});

bot.start();
```

Persist `event.cursor` after each handled event and reconnect with
`afterCursor` for exactly-once-ish processing. Ignore events whose
`payload.author_id` matches the bot's own user ID to avoid loops.

See [features/bots.md](features/bots.md) and [bot-installs.md](bot-installs.md).

## Example package

`examples/bot-ts` is a minimal one-shot bot that sends a single message:

```sh
CLICKCLACK_URL=http://localhost:8080 \
CLICKCLACK_TOKEN=ccb_... \
CLICKCLACK_CHANNEL_ID=chn_... \
CLICKCLACK_TEXT="clack from bot" \
pnpm --filter @clickclack/example-bot start
```
