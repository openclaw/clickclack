---
read_when:
  - adding bot accounts, bot tokens, or bot permissions
  - wiring ClickClack into OpenClaw as a channel extension
  - building long-running agents that subscribe to ClickClack realtime events
---

# Bots

ClickClack bots are first-class chat identities. They use the same message,
thread, DM, upload, search, and realtime surfaces as humans, but their
credentials and permissions are explicit, scoped, revocable, and visible in the
UI.

This spec covers two bot shapes:

- **Service bot**: an independent workspace member, not owned by a human. Use
  for shared infrastructure agents such as `openclaw`, `deploy-bot`, or
  `triage-bot`.
- **User bot**: a sub-identity owned by a human user. Use for "Peter's
  OpenClaw bot", personal assistants, or automation that should be visibly
  attached to a real person and limited to a subset of that person's access.

Both shapes are users with `kind=bot`. The difference is ownership and maximum
permission source.

## Goals

- Humans and bots are distinguishable in API responses and UI.
- Bots can post, reply, read, and subscribe without browser cookies or magic
  link human sessions.
- User-owned bots cannot exceed their owner's permissions.
- Service bots can exist without a human owner, but only through explicit
  workspace membership and scoped tokens.
- Token scopes are narrow enough for OpenClaw channel extensions and CI agents.
- OpenClaw can run as one service bot and as one user-owned bot in the same
  ClickClack workspace.
- Crabbox can prove the full path with a desktop/browser/WebVNC demo.

## Non-Goals

- OAuth app marketplace.
- Slack-compatible app manifests.
- Per-channel ACLs beyond the scopes below.
- Multi-tenant hosted billing or app review.
- Letting bot tokens impersonate arbitrary humans.

## Identity Model

`users` grows identity metadata:

```sql
kind TEXT NOT NULL DEFAULT 'human';
owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE;
```

Rules:

- `kind` is either `human` or `bot`.
- A human has `owner_user_id = NULL`.
- A service bot has `kind = bot` and `owner_user_id = NULL`.
- A user bot has `kind = bot` and `owner_user_id = <human user id>`.
- A bot can have its own `display_name`, `handle`, and `avatar_url`.
- A bot may not own another bot.
- Deleting a human owner revokes/deletes user-owned bots and tokens.
- `messages.author_id` stays a plain `users.id`. Deleting a bot retires that
  immutable ID instead of reassigning it, so old messages keep rendering while
  the former handle becomes available to a new bot ID.

API `User` payloads include:

```jsonc
{
  "id": "usr_...",
  "kind": "bot",
  "owner_user_id": "usr_owner...", // omitted for humans and service bots
  "display_name": "Peter's OpenClaw",
  "handle": "peter-openclaw",
}
```

Historical payloads for a deleted bot clear `handle` and add `former_handle`
plus `deleted_at`. A newly created bot that reuses the handle has a different
ID and does not inherit the deleted marker.

UI:

- Profile panes show a `Bot` badge for all bots.
- User-owned bot profiles show `Bot of <owner>`; the current UI uses the owner
  user ID until owner-profile hydration lands.
- Service bot profiles show `Service bot`.
- Sidebar People can either include bots with badges or split into `People` and
  `Bots`; the API must expose enough metadata for either UI.
- Historical messages, threads, quotes, search results, and DMs show the former
  handle with a `deleted bot` marker. Deleted identities are not profile or
  mention targets.

## Token Model

Bot tokens are bearer credentials with a `ccb_` prefix. The server stores only a
SHA-256 hash.

```sql
bot_tokens (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  bot_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  scopes_json TEXT NOT NULL,
  created_by TEXT REFERENCES users(id),
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at TEXT
)
```

Rules:

- Raw token is returned once on creation.
- Token auth resolves to the bot user, never to the owner.
- `owner_user_id` on the token is copied from the bot at creation for audit.
- Bundle names are expanded to concrete scopes when a token is issued. Bundle
  changes are not applied retroactively; rotate or mint a token to gain a newly
  added scope such as `commands:write`.
- Revoked tokens fail auth.
- Tokens update `last_used_at` after successful auth.
- Tokens are workspace-scoped. A token cannot access another workspace even if
  the bot user is later added there.
- A bot may have multiple tokens for different runtimes.

## Permission Scopes

MVP scopes:

- `workspaces:read`
- `channels:read`
- `channels:write`
- `messages:read`
- `messages:write`
- `agent_activity:write` (explicit only; excluded from all `bot:*` bundles)
- `threads:read`
- `threads:write`
- `dms:read`
- `dms:write`
- `realtime:read`
- `uploads:write`
- `profile:read`
- `commands:write`

Derived bundles:

- `bot:read` = workspace/channel/message/thread/DM/realtime read scopes.
- `bot:write` = read scopes plus message/thread/DM/upload writes and
  `commands:write`.
- `bot:admin` = all MVP scopes including channel creation/update.

Enforcement:

- Human sessions keep today's membership-based behavior.
- Bot-token requests check membership, token workspace, and scope.
- User-owned bots additionally require the owner to still be a member of the
  target workspace.
- Service bots require their own workspace membership.
- Store-level membership remains the final data access guard.

MVP endpoint mapping:

- `GET /api/me`: `profile:read`
- `GET /api/workspaces*`: `workspaces:read`
- `GET /api/workspaces/{id}/channels`: `channels:read`
- `POST/PATCH channel endpoints`: `channels:write`
- `GET channel/DM/thread messages`: matching read scope
- `POST channel/DM/thread messages`: matching write scope
- Durable `agent_commentary` and `agent_tool` channel/DM messages additionally
  require a bot token with `agent_activity:write`; human sessions and ordinary
  bot bundles cannot publish them.
- `GET /api/realtime/events` and `/ws`: `realtime:read`
- `PUT /api/bots/self/commands`: bot tokens only, with `commands:write`
- `GET /api/workspaces/{id}/bot-commands`: `workspaces:read`
- `POST /api/uploads`: `uploads:write`
- `POST /api/messages/{id}/attachments`: `uploads:write` and `messages:write`
- `POST /api/realtime/ephemeral`: `messages:write`; the `agent.progress`
  event type additionally requires a bot token and exactly one concrete target
  (`channel_id` or `direct_conversation_id`), never workspace-wide. DM progress
  also requires `dms:write`.
- `PATCH /api/me`: human sessions only; bot tokens cannot mutate profiles.

## Creation Surfaces

CLI MVP:

```sh
clickclack admin bot create \
  --workspace wsp_... \
  --created-by usr_manager \
  --name "OpenClaw Service" \
  --handle openclaw \
  --scopes bot:write \
  --plain

clickclack admin bot create \
  --workspace wsp_... \
  --owner usr_peter \
  --created-by usr_peter \
  --name "Peter's OpenClaw" \
  --handle peter-openclaw \
  --scopes bot:write \
  --plain
```

Plain output returns the raw token only. JSON output returns `{bot, token,
bot_token}`.

Service bot creation requires `--created-by` to be a workspace owner or
moderator. User-owned bot creation requires `--created-by` to match `--owner`;
workspace managers cannot mint or rotate tokens for someone else's user-owned
bot.

For the practical install flow, including Docker commands and OpenClaw
configuration, see [Bot installs](../bot-installs.md).

HTTP API:

- `POST /api/workspaces/{workspace_id}/bots`
- `GET /api/workspaces/{workspace_id}/bots`
- `DELETE /api/workspaces/{workspace_id}/bots/{bot_user_id}/membership`
- `GET /api/workspaces/{workspace_id}/bots/{bot_user_id}/tokens`
- `POST /api/workspaces/{workspace_id}/bots/{bot_user_id}/tokens`
- `POST /api/workspaces/{workspace_id}/bots/{bot_user_id}/setup-codes`
- `POST /api/bot-setup-codes/claim`
- `DELETE /api/bots/{bot_user_id}`
- `GET /api/bots/{bot_user_id}/tokens`
- `POST /api/bots/{bot_user_id}/tokens`
- `POST /api/bot-tokens/{token_id}/revoke`
- `GET /api/me/bots`
- `PUT /api/bots/self/commands`
- `GET /api/workspaces/{workspace_id}/bot-commands`

The creation and token-management API requires a human session. Bot tokens can
read and write through the normal chat APIs according to scope, but cannot mint,
list, or revoke bot tokens. The workspace-scoped token routes are preferred;
the legacy `/api/bots/{bot_user_id}/tokens` routes only work when the bot is
installed in exactly one workspace.

`POST /api/workspaces/{workspace_id}/bots` returns `{bot, bot_token}`. The
`bot_token.token` field is the one-time raw `ccb_...` token and is never
returned by list calls. `GET /api/workspaces/{workspace_id}/bots` returns
`{bots: [{bot, tokens}]}` with redacted token metadata for workspace members.
Raw token values are never returned by list calls. Rotation is create-new, move
the runtime, then revoke-old through
`POST /api/bot-tokens/{token_id}/revoke`.

## Setup Codes

Setup codes hand a token to an installer without the plaintext ever passing
through a clipboard or shell history. A setup code is a pending token grant:
`POST /api/workspaces/{workspace_id}/bots/{bot_user_id}/setup-codes` (same
authorization as token creation, human session required) returns a one-time
plaintext code (`XXXX-XXXX-XXXX`, 10-minute expiry) while storing only its
hash. No bot token exists yet.

The installer claims the code with the unauthenticated, rate-limited
`POST /api/bot-setup-codes/claim` (`{"code": "XXXX-XXXX-XXXX"}`). The claim
atomically consumes the code and mints the bot token at that moment,
returning `{token, bot, workspace, defaults}` with the one-time raw token,
minimal bot and workspace identity, and a suggested `defaultTo` channel when
one exists. Codes are single use;
unknown, expired, and already-claimed codes all answer with the same `404`.
Re-minting a code for the same bot and token name replaces the pending code,
and removing or deleting the bot invalidates its pending codes. An expired,
unclaimed code never creates a token.

The web app's token reveal panel (bot creation, token minting, and the
OpenClaw install wizard) generates a setup code automatically and shows the
one-liner `openclaw channels add clickclack --code "https://server/#XXXX-XXXX-XXXX"`
as the recommended connect path, with a countdown and one-click regeneration
after expiry. The raw token stays available for manual setups.

## Command Menus

Bots publish command discovery metadata with
`PUT /api/bots/self/commands`. The endpoint accepts bot tokens only, requires
`commands:write`, and derives the bot user and workspace from the token. It
atomically replaces that bot's full menu; `{"commands":[]}` clears it.

Commands accept an optional leading slash and are stored in lowercase canonical
form such as `/status`. A menu can contain at most 100 unique commands.
Descriptions are required and limited to 100 characters; optional `args_hint`
values are also limited to 100 characters. Any validation failure leaves the
previous menu unchanged.

Workspace members read the merged bot menus through
`GET /api/workspaces/{workspace_id}/bot-commands`. Bot tokens need
`workspaces:read` and must be bound to that workspace. Results embed the bot's
ID, handle, display name, and avatar, and are sorted by bot handle then command.
Removing a bot from a workspace deletes its menu in the same transaction.

The TypeScript SDK exposes these endpoints as
`client.bots.setCommands(commands)` and
`client.bots.listCommands(workspaceId)`.

Bot command menus are discovery metadata only. This backend contract does not
change dispatch or implement the web composer merge. The web integration's
precedence rule is that an HTTP-registered slash command wins when the same name
exists in both systems; bot-declared and unknown commands continue through
normal plain-message delivery. There is no cross-system uniqueness constraint.

Workspace owners and moderators can remove any bot from a workspace with
`DELETE /api/workspaces/{workspace_id}/bots/{bot_user_id}/membership`; this
removes the workspace membership and revokes that bot's tokens for that
workspace. The bot user row remains for history and future installs.

Deleting a bot is a separate global action through
`DELETE /api/bots/{bot_user_id}`. It revokes all tokens, app installations,
slash commands, event subscriptions, command menus, and workspace memberships
for that bot in one transaction, along with connected-account bindings owned by
the bot. It then records a tombstone and releases the active handle. User-owned
bots can be deleted only by their owner. Service bots require the requester to
be an owner or moderator in every workspace where the bot still has active
resources. If the bot is already orphaned with no active resources, deletion
falls back to every workspace found in its retained token, integration,
message, and DM history so an ordinary member cannot retire the global
identity.

Deletion returns `{deleted_bot: {id, display_name, former_handle, deleted_at}}`.
Repeating it returns `404`; it never targets a replacement bot that later
reuses the former handle because deletion is always by immutable bot ID.

## Runtime Contract

Long-running bots should:

1. Authenticate with `Authorization: Bearer ccb_...`.
2. Resolve workspace/channel IDs through normal APIs.
3. Backfill durable events through `/api/realtime/events?after_cursor=...`.
4. Connect to `/api/realtime/ws?workspace_id=...&after_cursor=...`.
5. Persist the latest cursor after each processed event.
6. Ignore events authored by their own `bot_user_id`.
7. Post replies through normal message/thread/DM endpoints.

The TypeScript SDK exposes `ClickClackBot`, a light runner around
`ClickClackClient` and the realtime WebSocket:

```ts
const bot = new ClickClackBot({
  baseUrl,
  token,
  workspaceId,
  afterCursor,
  onEvent: async (event, client) => {
    if (event.type === "message.created" && event.channel_id) {
      await client.channels.sendMessage(event.channel_id, { body: "ack" });
    }
  },
});
bot.start();
```

Runtimes are still responsible for reconnect backoff, cursor persistence, and
own-message filtering.

## OpenClaw Extension

OpenClaw should ship a ClickClack channel extension that treats ClickClack as a
normal chat transport.

Channel id: `clickclack`.

Config:

```jsonc
{
  "channels": {
    "clickclack": {
      "baseUrl": "https://app.clickclack.chat",
      "token": "$CLICKCLACK_BOT_TOKEN",
      "workspace": "clickclack",
      "defaultTo": "channel:general",
      "allowFrom": ["*"],
    },
  },
}
```

Target grammar:

- `channel:<name-or-id>`
- `dm:<user-id-or-handle>`
- `thread:<message-id>`

Inbound:

- The extension subscribes to ClickClack realtime events.
- `message.created` events become OpenClaw inbound turns.
- Events authored by the configured bot user are ignored.
- Channel messages map to OpenClaw group/channel turns.
- DMs map to direct turns.
- Thread replies preserve the source thread/message ID.

Outbound:

- OpenClaw replies post through ClickClack message/thread/DM endpoints.
- Thread context routes back to the originating ClickClack thread when present.
- The extension stores the latest realtime cursor per account.

The live proof must configure two ClickClack accounts:

- `openclaw-service`: independent service bot.
- `peter-openclaw`: user-owned bot with Peter as owner.

## Crabbox Live Proof

Success criteria:

- ClickClack tests pass locally and in CI-shaped gates.
- OpenClaw extension tests pass in the chosen OpenClaw checkout.
- A Crabbox managed Linux lease is created with `--desktop --browser`.
- ClickClack runs with bot identities and tokens.
- OpenClaw runs with an OpenAI API key and both ClickClack bot accounts.
- WebVNC opens the browser to ClickClack.
- The visible chat shows both bots distinctly:
  - one service bot
  - one bot of Peter
- A screenshot is captured with `crabbox screenshot`.
- The screenshot is inspected and confirms the expected chat/browser state.

Preferred Crabbox flow:

```sh
crabbox warmup --desktop --browser
crabbox run --id <lease> -- ./scripts/run-clickclack-openclaw-bot-smoke.sh
crabbox desktop launch --id <lease> --browser --url http://127.0.0.1:8080/app --webvnc --open
crabbox screenshot --id <lease> --output .artifacts/clickclack-bots-webvnc.png
```

Secrets:

- Load `OPENAI_API_KEY` from the local environment or approved secret source.
- Do not print bot tokens, session tokens, or OpenAI keys.
- Store raw bot tokens only in ephemeral Crabbox env files for the live test.

## Migration Plan

1. Add user kind/owner migration and backfill all existing users as humans.
2. Add bot token table and store methods.
3. Extend auth resolution to return actor metadata and token scopes.
4. Gate bot-token requests by scope in HTTP handlers.
5. Add CLI bot creation.
6. Add `kind`/`owner_user_id` to OpenAPI, SDK, and UI.
7. Add SDK bot runner and example.
8. Add OpenClaw ClickClack channel extension.
9. Add local and Crabbox live tests.
