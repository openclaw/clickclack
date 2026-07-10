---
read_when:
  - adding a CLI subcommand or flag
  - changing bootstrap, migrations, backup, or export behavior
---

# CLI

The single binary is `clickclack`. Source: `apps/api/cmd/clickclack/main.go`.
It can host a server and act as a scriptable local or remote chat client. The
broader agent-friendly contract is documented in
[agent-friendly-cli.md](agent-friendly-cli.md).

```text
clickclack <command> [flags]

Commands:
  serve      run the HTTP/WebSocket server (default if no command given)
  migrate    apply embedded SQL migrations
  admin      bootstrap, FakeCo seed, user create, invite create, bot create, events prune, magic-link create
  backup     write a SQLite backup file
  export     write a JSON dump to a file or stdout
  login      consume a magic-link token and store/print a session token
  logout     remove stored client credentials
  whoami     print the current server-side user
  status     print selected server/user/workspace/channel
  canary     prove a human ClickClack -> OpenClaw -> quoted bot reply round trip
  workspaces list
  channels list
  send
  messages send
  messages list
  threads open
  threads reply
```

Client commands accept these common flags before the command or on the command
itself:

| Flag | Env | Default | Notes |
| --- | --- | --- | --- |
| `--server` | `CLICKCLACK_SERVER` | `http://localhost:8080` | Remote server URL. |
| `--token` | `CLICKCLACK_TOKEN` | stored config | Sends `Authorization: Bearer`. |
| `--user`, `--user-id` | `CLICKCLACK_USER_ID` | unset | Sends `X-ClickClack-User`; local development/test escape hatch. |
| `--workspace` | `CLICKCLACK_WORKSPACE` | first visible workspace | ID, slug, or name. |
| `--channel` | `CLICKCLACK_CHANNEL` | `general`, then first visible channel | ID or name. Channel IDs are resolved across visible workspaces unless `--workspace` is set. |
| `--json` | — | false | Machine-readable JSON output. |
| `--plain` | — | false | Stable single-field output, usually an ID or token. |
| `--no-input` | — | false | Reserved for non-interactive flows. |

Stored client config lives at `~/.config/clickclack/config.json`. Stored
token, workspace, and channel defaults are scoped to their saved server URL.
If `--server` or `CLICKCLACK_SERVER` points somewhere else and no explicit
`--token` is supplied, the CLI will not reuse the saved bearer token. It also
will not reuse the saved workspace or channel unless the selected server
matches.

If `--user` or `CLICKCLACK_USER_ID` is set and no explicit `--token` is
supplied, the CLI also skips the stored bearer token so the server can honor
the requested dev user. If both `--token` and `--user` are supplied, the
server's auth order applies and the bearer token wins.

## `serve`

```sh
clickclack serve \
  --addr :8080 \
  --data ./data \
  --db sqlite://./data/clickclack.db \
  --config ./clickclack.json \
  --dev-bootstrap=true
```

- Loads config from `--config` (JSON) layered on top of env vars (`CLICKCLACK_*`).
  CLI flags win when explicitly set. See [configuration.md](configuration.md).
- Creates `<data>`, `<data>/uploads`, `<data>/logs`.
- Opens SQLite (modernc) with WAL, foreign keys, and `busy_timeout=5000`,
  then runs migrations.
- When `--dev-bootstrap=true`, creates a `Local Captain` user and a
  `ClickClack` workspace if the DB is empty. The default is `false`.
- Logs the resolved listen URL and the dev-auth user ID.
- `--environment` labels opt-in metrics; `--metrics-enabled=true` exposes them
  at `/metrics`.

## `migrate`

```sh
clickclack migrate --data ./data --db sqlite://./data/clickclack.db
```

Idempotent: each migration in
`apps/api/internal/store/sqlite/migrations/` is recorded in
`schema_migrations` and skipped on subsequent runs. Use this in
deployments before flipping traffic to a new build.

## `admin`

### `admin bootstrap`

```sh
clickclack admin bootstrap --name "Peter" --email steipete@gmail.com
```

Creates the first user, workspace, and `general` channel if none exist.
Idempotent — re-running prints the existing user's ID. Output is the
`usr_...` ID on stdout, ready to capture in a shell script.

### `admin user create`

```sh
clickclack admin user create --name "Ari" --email ari@example.com [--workspace wsp_...]
```

Creates a user. With `--workspace`, also adds them to that workspace as a
`member`. Prints the new user ID.

### `admin fakeco seed`

```sh
CLICKCLACK_ENVIRONMENT=fakeco clickclack admin fakeco seed --data ./data
```

Creates the deterministic synthetic FakeCo workspace, users, channels, roots,
and thread replies. Reruns reuse fixed identity subjects and message nonces.
The exact environment confirmation prevents accidental use against an
unlabelled or production deployment. The JSON output is a non-secret resource
manifest. See [fakeco.md](fakeco.md).

### `admin invite create`

```sh
clickclack admin invite create --workspace wsp_...
```

Mints an invite token (created by the first user in the DB, which is the
owner in single-tenant deployments). Prints the token. There is no consume
endpoint over HTTP yet — invite tokens are reserved for V1 work.

### `admin bot create`

```sh
clickclack admin bot create \
  --workspace wsp_... \
  --name "OpenClaw Service" \
  --handle openclaw \
  --scopes bot:write \
  --plain

clickclack admin bot create \
  --workspace wsp_... \
  --owner usr_peter \
  --name "Peter's OpenClaw" \
  --handle peter-openclaw \
  --scopes bot:write
```

Creates a `kind=bot` user, adds it to the workspace, and mints a scoped
`ccb_...` bot token. `--owner` makes it a user-owned bot; omitting `--owner`
makes it an independent service bot. Plain output prints only the raw token.
JSON output includes `{bot, bot_token, token}`. See
[features/bots.md](features/bots.md) and [bot-installs.md](bot-installs.md).

### `admin magic-link create`

```sh
clickclack admin magic-link create --email steipete@gmail.com --name "Peter"
```

Mints a magic-link token. Hand it to the user; they POST it to
`/api/auth/magic/consume` to get a session. See
[features/auth.md](features/auth.md).

### `admin events prune`

```sh
clickclack admin events prune \
  --workspace wsp_... \
  --older-than-days 30 \
  --keep-latest 10000
```

Deletes old durable realtime events for one workspace. At least one retention
bound is required: `--older-than-days`, `--before RFC3339`, or
`--keep-latest`. `--keep-latest` preserves the newest N events by cursor even
when they are older than the cutoff. Private event recipient rows are deleted
by cascade with their events.

## `backup`

```sh
clickclack backup --data ./data --out ./data/backup.db
```

Uses SQLite's online backup API to write a hot copy of the database. Safe to
run while `serve` is up. The destination must be on the same filesystem as
`<data>` if you want a fast atomic move afterwards.

## `export`

```sh
clickclack export --data ./data --out ./data/export.json
clickclack export --out -                # stdout
```

Writes a JSON dump of users, workspaces, channels, messages, threads,
reactions, uploads metadata, and DMs. Useful for migrations between SQLite
files or for one-off audits.

## Client auth

```sh
clickclack --server http://localhost:8080 login \
  --magic-token mgt_... \
  --plain
```

Consumes a magic-link token through `/api/auth/magic/consume`. By default it
stores the returned session token in the client config file. Use `--no-store`
for CI or tests that only need stdout.

`logout` removes the stored client config.

For remote or hosted agents, prefer `login` plus stored credentials or
`CLICKCLACK_TOKEN`. Reserve `--user` for local/dev servers where explicit user
impersonation is acceptable.

## Client reads

```sh
clickclack --server http://localhost:8080 --token sst_... whoami
clickclack --server http://localhost:8080 --token sst_... workspaces list
clickclack --server http://localhost:8080 --token sst_... channels list --workspace clickclack
clickclack --server http://localhost:8080 --token sst_... messages list --channel general --json
clickclack --server http://localhost:8080 --token sst_... threads open msg_... --json
```

`workspaces list` prints `id slug name` in human mode. `channels list` prints
`id name kind`. `messages list` prints `seq id author body`.

## Client writes

```sh
clickclack --server http://localhost:8080 --token sst_... send --channel general "hello"
printf 'long body\n' | clickclack --server http://localhost:8080 --token sst_... send --channel general --stdin
clickclack --server http://localhost:8080 --token sst_... threads reply msg_... --stdin <reply.md
```

`send` and `threads reply` accept the body from a positional argument,
`--body`, `--file`, or `--stdin`. Both also accept `--reply-to msg_...` to
inline-quote an existing message in the same channel/thread (see
[features/replies.md](features/replies.md)). `--plain` prints only the
created message ID; `--json` prints the API response.

## `canary`

```sh
CLICKCLACK_TOKEN=... \
OPENCLAW_GATEWAY_HEALTH_URL=http://openclaw.internal:18789/healthz \
clickclack canary --workspace fakeco --channel e2e-canary --json
```

Requires a human session token. It optionally preflights the OpenClaw health
URL, posts a unique correlated prompt, and waits for a bot reply quoting the
request and carrying the same marker. `--timeout`, `--poll-interval`, and
`--correlation-id` are available for controlled tests. Never pass credentials
inside the health URL.

## Exit codes

`0` on success, non-zero on any error (a bare `log.Fatal` in `main`). Scripts
should rely on the exit code, not the log line format.
