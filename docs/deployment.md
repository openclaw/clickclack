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
2. `golang:1.26.5-alpine` — builds the Go binary, importing the SPA dist.
3. `alpine:3.23` — runtime image, runs as the `clickclack` user, exposes
   `8080`, mounts `/app/data` as a volume.

Override the entrypoint command to run admin tasks:

```sh
docker run --rm -v clickclack-data:/app/data clickclack \
  admin bootstrap --name "Peter" --email steipete@gmail.com
```

For the isolated non-production small-VM topology, deterministic synthetic
seed, OpenClaw/ClawRouter SecretRef contract, canary, and teardown, see
[FakeCo staging](fakeco.md). It uses this same Docker image and SQLite adapter;
it does not add a second ClickClack cloud runtime. The guarded FakeCo-only AWS
owner is documented in [deploy/fakeco/aws/README.md](../deploy/fakeco/aws/README.md).

## Health and telemetry

`GET /healthz` reports process liveness. `GET /readyz` checks database
connectivity and returns `503` without the database error when unavailable.
Both responses include an `X-Correlation-ID`; callers may supply a safe ID or
let the server generate one.

Set `CLICKCLACK_METRICS_ENABLED=true` only on a private operator network to
expose Prometheus text at `/metrics`. Metrics use normalized route patterns and
status classes; they do not label users, workspaces, channels, messages, query
values, or body content. When disabled, `/metrics` returns `404`.

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

## Hosted deployment

The official hosted ClickClack deployment is operated separately from the
self-hosting examples in this document. Hosted edge configuration is outside
this repository change; the OAuth and OpenAPI changes described here do not
modify or prove it. Hosted edge controls, rollout, and verification must be
handled in the infrastructure that owns hosted traffic.

An OpenAPI `429` response documents a response that a deployment edge may
return when configured. It is not evidence that the hosted deployment currently
enforces an edge rate limit.

## Self-hosting reverse proxy

An internet-facing self-hosted deployment needs a trusted reverse proxy or
equivalent edge for TLS and request-size enforcement. The WebSocket endpoint
enforces the request host by default and also allows `CLICKCLACK_PUBLIC_URL` as
an origin when configured.

Terminate TLS only at infrastructure you trust, redirect HTTP to HTTPS, and
send HSTS from the public HTTPS origin after confirming every subdomain covered
by the policy is HTTPS-ready. ClickClack intentionally does not trust arbitrary
`X-Forwarded-For` values for security decisions. The setup-code claim limiter
uses the transport peer by default. It accepts a forwarded client IP only when
the peer is loopback and `X-Real-IP` matches a single-IP `X-Forwarded-For`,
which is the contract used by the bundled Nginx example. Other proxy topologies
must enforce their own edge limit or expose a separately verified client
identity; arbitrary forwarding headers remain ignored.

### Optional self-hosted Nginx example

[`deploy/nginx/clickclack.conf.example`](../deploy/nginx/clickclack.conf.example)
is an optional starting point for a directly operated, self-hosted Nginx
deployment. It is not ClickClack's hosted production configuration and does not
describe or configure the hosted edge.

The example includes TLS, WebSocket proxying, a 64 MiB request limit,
query-free access logs, explicit forwarding headers, and OAuth plus setup-code
claim rate limits.
Replace its hostname and certificate paths, confirm the upstream address, run
`nginx -t`, and reload only after the syntax check succeeds. Its OAuth start
bucket allows one request per second with a burst of eight, while desktop
redemption allows five requests per second with a burst of twenty. These are
self-hosting starting values, not universal capacity targets.

The limits use the directly connected client IP. If another proxy sits in front
of Nginx, configure `ngx_http_realip_module` with only that proxy's published
address ranges before relying on the limits. Never accept an arbitrary
client-supplied forwarding header as the rate-limit key.

## GitHub OAuth

If you want GitHub login, set:

```sh
CLICKCLACK_PUBLIC_URL=https://chat.example.com
# Optional only when trusted ClickClack instances share this hostname:
# CLICKCLACK_COOKIE_NAMESPACE=production
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

`CLICKCLACK_PUBLIC_URL` is startup-validated as an exact origin. Non-loopback
origins must use HTTPS and cannot contain a path, credentials, query, or
fragment. GitHub OAuth credentials are rejected without it.

GitHub OAuth apps have one configured callback URL. Each ClickClack public
origin, including a distinct non-default port, therefore needs matching OAuth
app configuration. Never point multiple unrelated public origins at one
instance and rely on the request `Host` header to choose a callback.

HTTP cookies do not include ports in their scope. When multiple trusted
ClickClack instances share one hostname, assign each a unique, stable
`CLICKCLACK_COOKIE_NAMESPACE`; all replicas of one instance must use the same
value. This avoids accidental cookie-name collisions but does not isolate
mutually untrusted services. Use separate hostnames, or separate registrable
domains for stronger isolation.

OAuth state and desktop grants are stored in the configured database, so
callbacks can land on a different replica and survive process restarts.
If a deployment configures edge rate limiting, cover:

- `GET /api/auth/github/start`
- `GET /api/auth/github/desktop/start`
- `POST /api/auth/github/desktop/consume`

Use a client identity that is trustworthy for the complete deployment path and
leave enough headroom for legitimate users behind shared networks. The Go
server does not implement IP-based OAuth limiting because it cannot assume that
the directly connected address or a forwarded header identifies the original
client in every deployment.

The database rejects new work at 8,192 pending OAuth transactions, eight
pending starts per browser binding, and 4,096 pending desktop grants. These are
capacity safeguards, not recommended edge rate limits. Monitor pending capacity
and do not raise the limits without load testing.

When edge limiting is configured, alert on sustained `429` responses. Always
alert on sustained `503` or
`clickclack_github_oauth_events_total{event="capacity_rejected"}` activity, and
on pending capacity before it reaches the hard limit.

Configure proxy and edge logs to omit query strings on every GitHub OAuth
route, including `/api/auth/github/callback`. Query strings can contain
short-lived authorization codes or desktop verifier challenges. Never log
`Authorization`, `Cookie`, or `Set-Cookie` headers. ClickClack's request logger
records route patterns without query strings.

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
