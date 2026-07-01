---
read_when:
  - shipping a release, building Docker, or planning a new install
  - changing how data is laid out on disk
---

# Deployment

ClickClack ships as one Go binary that embeds the Svelte SPA and the SQL
migrations. The deployment story is "drop a binary on a box, point it at a
data directory, run it behind a reverse proxy."

Public surfaces:

- `clickclack.chat` — product website.
- `app.clickclack.chat` — chat app. The same app is also available at `/app`
  for local development and simple single-host deployments.
- `docs.clickclack.chat` — documentation site built by `pnpm docs:site`.

## Single binary

```sh
pnpm install
pnpm build                                          # builds the SPA into apps/api/internal/webassets/dist
go build -o clickclack ./apps/api/cmd/clickclack
./clickclack serve --addr :8080 --data /var/lib/clickclack
```

The Go build step requires the SPA `dist/` to be present because `webassets`
uses `go:embed`. The `pnpm build` script copies `apps/web/dist` into
`apps/api/internal/webassets/dist`; CI must run it before `go build`.

The embedded frontend is a SvelteKit static SPA. Reverse proxies should pass
unknown paths through to the ClickClack binary, because direct visits to app
routes such as `/app/T.../C...`, `/app/T.../D...`, and `/app/T.../M...` are
resolved by the frontend fallback. Older internal-ID links such as
`/app/wsp_.../chn_...`, `/app/wsp_.../dm_...`, and `/app/wsp_.../msg_...`
are still accepted and canonicalized by the app after API permission checks.

`pnpm build` defaults the SvelteKit app version to `dev` so repeated local
builds do not rewrite embedded asset filenames when source code has not
changed. Release automation should set `CLICKCLACK_WEB_VERSION` to the commit
or tag being shipped so long-lived open browser tabs can detect a newly
deployed frontend bundle:

```sh
CLICKCLACK_WEB_VERSION="$(git rev-parse --short=12 HEAD)" pnpm build
```

## Releases

GoReleaser is configured in `.goreleaser.yml`. It builds `clickclack` for
Linux, macOS, Windows, and FreeBSD on `amd64` and `arm64`, with Windows
archives emitted as `.zip` and the others as `.tar.gz`. Linux `.deb` and
`.rpm` packages are generated through nfpm.

```sh
pnpm install
CLICKCLACK_WEB_VERSION="$(git rev-parse --short=12 HEAD)" \
  goreleaser release --snapshot --clean
```

The GoReleaser config runs `pnpm build` before compiling so the embedded SPA
is refreshed. The GitHub release workflow sets `CLICKCLACK_WEB_VERSION` from
the checked-out commit before invoking GoReleaser. Publishing is handled by
`.github/workflows/release.yml` on `v*` tags or manual dispatch with an
existing tag.

## Docker

The provided `Dockerfile` is multi-stage:

```sh
docker build \
  --build-arg CLICKCLACK_WEB_VERSION="$(git rev-parse --short=12 HEAD)" \
  -t clickclack .
docker run --rm -p 8080:8080 -v clickclack-data:/app/data clickclack
```

Stages:

1. `node:24-alpine` — installs pnpm dependencies and runs `pnpm build`.
2. `golang:1.26-alpine` — builds the Go binary, importing the SPA dist.
3. `alpine:3.23` — runtime image, runs as the `clickclack` user, exposes
   `8080`, mounts `/app/data` as a volume.

Override the entrypoint command to run admin tasks:

```sh
docker run --rm -v clickclack-data:/app/data clickclack \
  admin bootstrap --name "Peter" --email steipete@gmail.com
```

## Data layout

SQLite layout:

```
<data>/
  clickclack.db                  # SQLite database (WAL files alongside)
  uploads/                       # local files for /api/uploads
  logs/                          # reserved; nothing writes here today
```

Back this directory up. SQLite WAL means a snapshot of the directory is
consistent enough, but prefer the online backup:

```sh
clickclack backup --data /var/lib/clickclack --out /var/backups/clickclack-$(date +%F).db
```

Postgres layout:

```sh
CLICKCLACK_DB='postgres://user:pass@db.example.com:5432/clickclack?sslmode=require' \
clickclack serve --addr :8080 --data /var/lib/clickclack
```

The Postgres adapter stores users, messages, events, auth, search, and chat
metadata in Postgres. Use provider snapshots or `pg_dump` for Postgres
backups; `clickclack backup` is SQLite-only.

R2 upload layout:

```sh
CLICKCLACK_DB='postgres://user:pass@db.example.com:5432/clickclack?sslmode=require' \
CLICKCLACK_UPLOADS='r2://clickclack-uploads/prod' \
CLICKCLACK_R2_ACCOUNT_ID='91b59577e757131d68d55a471fe32aca' \
CLICKCLACK_R2_ACCESS_KEY_ID='...' \
CLICKCLACK_R2_SECRET_ACCESS_KEY='...' \
CLICKCLACK_DEV_BOOTSTRAP=false \
clickclack serve --addr :8080 --data /var/lib/clickclack
```

R2 stores upload bytes; Postgres stores upload metadata and message attachment
links. Requests still go through `/api/uploads/{id}` so workspace/member
authorization stays server-side.

## Reverse proxy

Required for TLS and request size limits. The WebSocket endpoint enforces the
request host by default and also allows `CLICKCLACK_PUBLIC_URL` as an origin
when configured.

A minimal nginx block:

```nginx
location / {
  proxy_pass http://127.0.0.1:8080;
  proxy_set_header Host $host;
  proxy_http_version 1.1;
  proxy_set_header Upgrade $http_upgrade;
  proxy_set_header Connection "upgrade";
  proxy_read_timeout 300s;
}
```

## GitHub OAuth

If you want GitHub login, set:

```sh
CLICKCLACK_PUBLIC_URL=https://chat.example.com
CLICKCLACK_GITHUB_CLIENT_ID=...
CLICKCLACK_GITHUB_CLIENT_SECRET=...
# Optional org gate:
# CLICKCLACK_GITHUB_ALLOWED_ORG=openclaw
# Optional moderator org for open guest login:
# CLICKCLACK_GITHUB_MODERATOR_ORG=openclaw
```

Configure the GitHub OAuth app callback to
`<public-url>/api/auth/github/callback`. Without `CLICKCLACK_GITHUB_ALLOWED_ORG`,
any GitHub account can sign in and is joined to an isolated `Guests` workspace
with a `guest` channel. When `CLICKCLACK_GITHUB_MODERATOR_ORG` is set, members
of that org become guest-workspace moderators and non-members start as
post-limited guests until approved. When the org gate is set, ClickClack asks
GitHub for `read:org` and only accepts active members of that org. See
[features/auth.md](features/auth.md) and
[features/moderation.md](features/moderation.md).

## Migrations

`clickclack serve` applies migrations on boot. For zero-downtime deploys, run
`clickclack migrate` ahead of the new binary so the old binary doesn't see
unexpected tables. SQLite migrations live in
`apps/api/internal/store/sqlite/migrations/`; Postgres migrations live in
`apps/api/internal/store/postgres/migrations/`. Both are append-only.

## Event retention

The durable realtime event log is for reconnect recovery, not permanent
message history. Message history stays in `messages`; old events can be
removed after the offline-recovery window you operate against:

```sh
clickclack admin events prune --workspace wsp_... --older-than-days 30 --keep-latest 10000
```

Run this from maintenance automation after backups. Clients with cursors older
than the retained event window should resync through the message APIs.

## Backups and restore

```sh
# hot backup
clickclack backup --out /var/backups/clickclack-$(date +%F).db

# JSON dump (good for sanity, not for restore)
clickclack export --out /var/backups/clickclack-$(date +%F).json
```

SQLite restore is a file swap: stop `clickclack`, replace
`<data>/clickclack.db`, delete any stale `*.db-wal`/`*.db-shm`, start it back
up. Postgres restore uses your database provider's restore flow or `psql`
from a `pg_dump` output.
