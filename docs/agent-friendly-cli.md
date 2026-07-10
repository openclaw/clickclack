---
read_when:
  - designing CLI commands for agents, scripts, or humans
  - adding remote client behavior to the Go binary
  - changing stdout, stderr, JSON, config, or exit-code contracts
---

# Agent-Friendly CLI

ClickClack should be agent-friendly without embedding an LLM runtime. The Go
binary owns a stable command surface that humans, scripts, CI jobs, and external
agents can drive. Model providers and agent loops live outside ClickClack.

The same binary has two roles:

- `clickclack serve` hosts a ClickClack server.
- `clickclack ...` without `serve` acts as a local or remote chat client.

This page is the target contract for that client CLI. The implemented subset is
tracked in [cli.md](cli.md).

## Design Goals

- Pleasant for humans in a terminal.
- Predictable for agents and shell scripts.
- Works against `localhost` and hosted servers with the same commands.
- Uses the public HTTP/WebSocket API; no private server backdoors.
- Supports durable cursor recovery for long-running watchers.
- Keeps all LLM/provider behavior outside the ClickClack binary.

## Command Shape

Implemented now:

- `login --magic-token`, `logout`, `whoami`, `status`.
- `workspaces list`.
- `channels list`.
- `send`, `messages send`, `messages list`.
- `threads open`, `threads reply`, and `reply`.
- `canary` for a correlated human-to-OpenClaw quoted-reply check.
- `--server`, `--token`, `--user` / `--user-id`, `--workspace`, `--channel`,
  `--json`, `--plain`, `--no-input`, and `--verbose`.

Still target-only:

- `workspaces use`, `channels use`, `channels create`.
- `messages tail`, `messages watch`, `events tail`.
- reactions from the CLI.
- Stable non-generic exit-code mapping.

```text
clickclack [global flags] <command> [args]

Server:
  serve                         run the HTTP/WebSocket server
  migrate                       apply embedded SQL migrations
  backup                        write a SQLite backup file
  export                        write a JSON dump

Client:
  login                         authenticate with a server
  logout                        remove stored credentials
  whoami                        print the current caller
  status                        print server, auth, workspace, and channel
  canary                        prove an OpenClaw quoted-reply round trip

Workspaces:
  workspaces list               list visible workspaces
  workspaces use <workspace>    set the default workspace

Channels:
  channels list                 list channels in the current workspace
  channels use <channel>        set the default channel
  channels create <name>        create a channel

Messages:
  send <body>                   send to the default channel
  messages send <body>          send to a channel
  messages list                 list recent channel messages
  messages tail                 stream new channel messages
  messages watch                stream durable events

Threads:
  threads open <message-id>     list a message thread
  threads reply <message-id>    reply in a thread

Reactions:
  reactions add <message-id> <emoji>
  reactions remove <message-id> <emoji>

Events:
  events tail                   stream workspace events
```

Short aliases are allowed for the high-frequency path:

```sh
clickclack send "deploy started"
clickclack tail --channel ops
clickclack reply msg_... --stdin <summary.md
```

## Global Flags

| Flag | Meaning |
| --- | --- |
| `--server <url>` | Server URL. Defaults to config, then `http://localhost:8080`. |
| `--workspace <id-or-slug>` | Workspace override. |
| `--channel <id-or-name>` | Channel override. |
| `--token <token>` | Authentication token. Prefer env or stored credentials for regular use. |
| `--user <user-id>` | Local/dev user override. Skips stored bearer tokens unless `--token` is explicit. |
| `--json` | Emit JSON instead of human text. |
| `--plain` | Emit stable line-oriented text where useful. |
| `--no-input` | Disable prompts. Required for non-interactive automation. |
| `--quiet` | Suppress non-essential diagnostics. |
| `--verbose` | Print request IDs, selected config, and reconnect diagnostics to stderr. |
| `--no-color` | Disable ANSI color. Also respect `NO_COLOR` and `TERM=dumb`. |

`-h` and `--help` always show help and ignore other flags. `--version` prints
the binary version to stdout.

## Config

Current client resolution order:

1. CLI flags.
2. Environment variables.
3. User config.
4. Defaults.

Environment variables:

```sh
CLICKCLACK_SERVER=https://clickclack.chat
CLICKCLACK_TOKEN=ses_...
CLICKCLACK_USER_ID=usr_dev
CLICKCLACK_WORKSPACE=wsp_...
CLICKCLACK_CHANNEL=chn_...
```

User config:

```json
{
  "server": "https://clickclack.chat",
  "token": "ses_...",
  "workspace": "main",
  "channel": "general"
}
```

The implemented client stores this at `~/.config/clickclack/config.json`.
Stored bearer tokens, workspace defaults, and channel defaults are scoped to
their saved server URL. If a command points at another server, the saved token
is not sent unless `--token` or `CLICKCLACK_TOKEN` explicitly provides one;
workspace/channel defaults from the saved server are ignored.

`--user` / `CLICKCLACK_USER_ID` is only for local/dev impersonation. When it is
set without an explicit token, the CLI skips any stored bearer token so the
server sees `X-ClickClack-User`. For hosted agents, use a real session token.

## Input

Message body precedence:

1. Positional body argument.
2. `--body <text>`.
3. `--file <path>`.
4. `--stdin`.

Examples:

```sh
clickclack send "hello from a script"
clickclack messages send --channel ops --file incident.md
printf 'build passed\n' | clickclack send --stdin
```

Non-interactive commands must never prompt unless stdin is a TTY and
`--no-input` is not set.

## Output

Primary data goes to stdout. Diagnostics, progress, reconnect notices, and
warnings go to stderr.

Human output should be compact:

```text
sent msg_01kr... to #ops
```

`--json` emits one JSON object for finite commands:

```json
{"message":{"id":"msg_01kr...","channel_id":"chn_...","body":"hello"}}
```

Streaming commands use newline-delimited JSON:

```json
{"type":"message.created","cursor":"evt_...","message":{"id":"msg_..."}}
{"type":"thread.reply_created","cursor":"evt_...","message":{"id":"msg_..."}}
```

Agents should use `--json` for commands that parse output and `--plain` only
for stable single-field output such as IDs or tokens.

## Realtime Behavior

`messages tail`, `messages watch`, and `events tail` use the same recovery
model as the web client:

1. Load the saved cursor for the selected server/workspace/channel.
2. Fetch missed durable events with `/api/realtime/events?after_cursor=...`.
3. Open `/api/realtime/ws`.
4. Persist the last delivered cursor after each event.
5. If the server returns `resync_required`, refetch state and replace the saved
   cursor.

Default cursor state belongs under:

```text
~/.local/state/clickclack/cursors/
```

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | Generic failure. |
| `2` | Invalid usage or validation failure. |
| `3` | Authentication required or invalid credentials. |
| `4` | Requested workspace, channel, message, or thread was not found. |
| `5` | Authenticated but not allowed. |
| `10` | Network unavailable or server unreachable. |
| `11` | Server returned an unexpected response. |

Scripts should branch on exit codes, not error text.

## Agent Examples

Post a message to a hosted server:

```sh
CLICKCLACK_SERVER=https://clickclack.chat \
CLICKCLACK_TOKEN=ses_... \
clickclack send --channel ops "release started"
```

Watch channel messages as JSON:

```sh
clickclack messages tail --channel ops --json --no-input
```

Reply to a thread with generated text from another process:

```sh
agent-summarize incident.log | clickclack threads reply msg_01kr... --stdin
```

Bridge CI output into a channel:

```sh
if pnpm test; then
  clickclack send --channel builds "tests passed"
else
  clickclack send --channel builds "tests failed"
fi
```

Fetch recent messages for context:

```sh
clickclack messages list --channel support --limit 20 --json
```

## Implementation Order

1. `login`, `logout`, `whoami`, and credential storage.
2. `workspaces list/use` and `channels list/use`.
3. `send` and `messages list`.
4. `messages tail --json` with durable cursor recovery.
5. `threads open/reply`.
6. Shell completions and stricter exit-code mapping.
