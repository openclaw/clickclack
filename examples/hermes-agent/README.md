# Hermes Agent connector

This example runs a Hermes Agent as a first-class ClickClack bot. It is an
external bridge: ClickClack stays the chat system of record, while Hermes runs
and executes tools on the Hermes server.

## Behavior

- Direct messages dispatch only when the sender ID is explicitly allowed.
- Channel messages require both an allowed sender ID and an allowed channel ID;
  top-level messages must also mention the bot handle.
- The first channel answer is posted in a thread.
- Later human replies continue without another mention while the bot appears in the
  latest 200 replies; older dormant threads require a fresh mention.
- Up to 20 prior DM or thread messages are rebuilt as Hermes conversation
  history by default. No connector-side conversation database is needed.
- Messages in one DM/thread are serialized for coherent history. Different
  conversations run concurrently, with a configurable global limit.
- ClickClack receives ephemeral lifecycle and tool-name progress. Raw Hermes
  reasoning, tool arguments, and tool previews are never published.
- Final replies use a deterministic ClickClack message nonce derived from the
  source message ID.

On the first process start, the connector captures and atomically persists
ClickClack's current event tail, so it does not replay old messages. Reconnects and
process restarts resume from the last contiguous successfully completed cursor.
Socket read-ahead is capped at 32 durable events; hitting the cap closes and
resumes the socket from that committed cursor.

Delivery is at least once across crashes. If the process exits after Hermes
finishes but before the cursor file is committed, that event is replayed. The
deterministic ClickClack reply nonce prevents a duplicate final message, but a
side-effecting Hermes tool may run again. Use Hermes approval policies and
idempotent tools for externally mutating work.

## Requirements

- Node.js 24 or newer.
- A ClickClack server and workspace ID.
- A ClickClack bot token with `bot:write`.
- A current [Hermes Agent API server](https://hermes-agent.nousresearch.com/docs/user-guide/features/api-server)
  exposing the Runs API and SSE events.

The connector checks `/v1/capabilities` at startup and exits if the Hermes
server lacks the required Runs contract.

## 1. Create the ClickClack bot

Run this against the ClickClack data directory:

```sh
clickclack admin bot create \
  --data /var/lib/clickclack \
  --workspace wsp_... \
  --created-by usr_manager \
  --name "Hermes Agent" \
  --handle hermes \
  --scopes bot:write \
  --token-name hermes-connector \
  --plain
```

The command reveals the `ccb_...` token once. Put it directly in the connector's
secret store. Do not commit it or paste it into logs.

## 2. Enable the Hermes API server

Set a strong API key in the Hermes host's `~/.hermes/.env`:

```sh
API_SERVER_ENABLED=true
API_SERVER_KEY=replace-with-a-long-random-secret
```

Then start Hermes:

```sh
hermes gateway
```

The default API listener is `http://127.0.0.1:8642`. Keep Hermes on loopback
when the connector runs on the same host. For separate hosts, use a private
network or authenticated TLS reverse proxy and keep `API_SERVER_KEY` enabled.
Remote Hermes URLs must use HTTPS unless
`HERMES_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP=true` is explicitly set for a
trusted development network. The connector is server-to-server and does not
need CORS.

Remote ClickClack URLs also require HTTPS because the connector sends the bot
token and conversation data to that origin. For a trusted development network
only, set `CLICKCLACK_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP=true`.

## 3. Run the connector

From the ClickClack repository:

```sh
pnpm install --frozen-lockfile

CLICKCLACK_BASE_URL=https://app.clickclack.chat \
CLICKCLACK_BOT_TOKEN=ccb_... \
CLICKCLACK_WORKSPACE_ID=wsp_... \
HERMES_CONNECTOR_ALLOWED_USER_IDS=usr_manager \
HERMES_CONNECTOR_ALLOWED_CHANNEL_IDS=chn_ops \
HERMES_CONNECTOR_CURSOR_FILE=/var/lib/clickclack-hermes/cursor.json \
HERMES_API_URL=http://127.0.0.1:8642 \
HERMES_API_KEY=replace-with-a-long-random-secret \
pnpm --filter @clickclack/example-hermes-agent start
```

For a local loopback Hermes server with authentication intentionally disabled,
`HERMES_API_KEY` may be omitted. The connector refuses an unauthenticated
non-loopback Hermes URL.

## Configuration

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `CLICKCLACK_BASE_URL` | yes | — | ClickClack HTTP origin |
| `CLICKCLACK_BOT_TOKEN` | yes | — | Workspace-scoped `ccb_...` token |
| `CLICKCLACK_WORKSPACE_ID` | yes | — | Workspace watched by the bot |
| `CLICKCLACK_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP` | no | `false` | Allow remote plaintext ClickClack only on trusted dev networks |
| `HERMES_CONNECTOR_ALLOWED_USER_IDS` | yes | — | Comma-separated human user IDs allowed to invoke Hermes; wildcards are rejected |
| `HERMES_CONNECTOR_ALLOWED_CHANNEL_IDS` | no | empty | Comma-separated channel IDs allowed to invoke Hermes; empty denies every channel |
| `HERMES_CONNECTOR_CURSOR_FILE` | yes | — | Absolute path for workspace-bound durable cursor state |
| `HERMES_API_URL` | no | `http://127.0.0.1:8642` | Hermes API server origin |
| `HERMES_API_KEY` | remote Hermes only | — | Hermes bearer key |
| `HERMES_CONNECTOR_HISTORY_LIMIT` | no | `20` | Prior DM/thread messages, `0-200` |
| `HERMES_CONNECTOR_MAX_REPLY_CHARS` | no | `100000` | Final reply truncation boundary |
| `HERMES_CONNECTOR_MAX_CONCURRENT_RUNS` | no | `4` | Concurrent runs across conversations, `1-32` |
| `HERMES_CONNECTOR_RUN_TIMEOUT_MS` | no | `1800000` | Run/approval timeout, `1000-86400000` ms |
| `HERMES_CONNECTOR_RECONNECT_MS` | no | `2000` | Reconnect delay, `50-60000` ms |
| `HERMES_CONNECTOR_ALLOW_INSECURE_REMOTE_HTTP` | no | `false` | Allow remote plaintext Hermes only on trusted dev networks |
| `HERMES_CONNECTOR_INSTRUCTIONS` | no | built in | Per-run ClickClack instructions |

## Approvals

Hermes may pause dangerous tool calls for approval. ClickClack does not yet
have a native approval-response UI, so the connector publishes an
`Approval required in Hermes` progress line and leaves the run waiting. Approve
or deny it through another trusted Hermes client. The connector never
auto-approves commands. A waiting approval is cancelled when the configured run
timeout expires, freeing its concurrency slot.
Stopping a timed-out run is best-effort and independently bounded to five
seconds, so an unresponsive Hermes stop endpoint cannot pin the slot.

## Operational notes

- Run one connector process per bot token/workspace pair.
- Keep the cursor file on persistent local storage. Give the connector user write
  access to its parent directory; cursor files are created with mode `0600`.
- Do not reuse a cursor file across workspaces. The connector fails closed when
  the stored workspace ID differs from `CLICKCLACK_WORKSPACE_ID`.
- Use a process supervisor and restart on non-zero exit.
- Rotate the ClickClack and Hermes keys independently.
- Do not expose an unsandboxed Hermes API directly to the public internet.
- Progress delivery is best-effort; a progress failure never suppresses the
  final ClickClack reply. Final replies retry three times with one deterministic
  nonce and are marked completed only after durable delivery succeeds.
- A generic failure appears in chat. Detailed errors stay in connector logs so
  provider diagnostics and internal paths are not copied into ClickClack.

## Tests

```sh
pnpm --filter @clickclack/example-hermes-agent test
pnpm --filter @clickclack/example-hermes-agent typecheck
```

The real-server integration proof requires Go and built ClickClack web assets.
From the repository root:

```sh
pnpm build:web
pnpm build:sdk
pnpm test:hermes-agent-e2e
```
