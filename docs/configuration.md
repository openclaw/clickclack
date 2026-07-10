---
read_when:
  - adding a config knob
  - changing precedence between flag, env, and config file
---

# Configuration

`clickclack serve` resolves config in this order. Later sources override
earlier ones for any given key:

1. Hard-coded defaults (`Addr=":8080"`, `Data="./data"`).
2. Environment variables.
3. JSON config file passed via `--config`.
4. CLI flags that were explicitly set.

Source: `apps/api/internal/config/config.go` and the `applyFlagOverrides`
hook in `cmd/clickclack/main.go`.

## Flags and env vars

| Flag                  | Env                              | Default     | Notes |
|-----------------------|----------------------------------|-------------|-------|
| `--addr`              | `CLICKCLACK_ADDR`                | `:8080`     | HTTP listen address. |
| `--data`              | `CLICKCLACK_DATA`                | `./data`    | Data root for DB, uploads, logs. |
| `--db`                | `CLICKCLACK_DB`                  | derived     | DB URL. Defaults to `sqlite://<data>/clickclack.db`. |
| `--uploads`           | `CLICKCLACK_UPLOADS`             | derived     | Upload storage URL. Defaults to `file://<data>/uploads`; use `r2://bucket/prefix` for Cloudflare R2. |
| `--environment`       | `CLICKCLACK_ENVIRONMENT`         | unset       | Low-cardinality deployment label used only by opt-in metrics. |
| `--metrics-enabled`   | `CLICKCLACK_METRICS_ENABLED`     | `false`     | Expose metadata-only Prometheus metrics at `/metrics`; keep private. |
| `--config`            | —                                | unset       | JSON config file. |
| `--dev-bootstrap`     | `CLICKCLACK_DEV_BOOTSTRAP`       | `false`     | `serve` only. Creates a default user/workspace/channel and enables local dev auth fallbacks when explicitly set to `true`. |
| —                     | `CLICKCLACK_PUBLIC_URL`          | unset       | External URL. Used to build the GitHub OAuth callback. |
| —                     | `CLICKCLACK_GITHUB_CLIENT_ID`    | unset       | GitHub OAuth app client ID. |
| —                     | `CLICKCLACK_GITHUB_CLIENT_SECRET`| unset       | GitHub OAuth app client secret. |
| —                     | `CLICKCLACK_GITHUB_ALLOWED_ORG`  | unset       | Optional GitHub org login gate. Requires `read:org` scope. |
| —                     | `CLICKCLACK_GITHUB_MODERATOR_ORG`| unset       | Optional GitHub org whose members become guest-workspace moderators. Requires `read:org` scope. |
| —                     | `CLICKCLACK_PUSHOVER_API_TOKEN`  | unset       | Pushover application API token. Users still opt in with their own Pushover user key in account settings. |
| —                     | `CLICKCLACK_R2_ACCOUNT_ID`       | unset       | Cloudflare account ID for `r2://` uploads. |
| —                     | `CLICKCLACK_R2_ACCESS_KEY_ID`    | unset       | R2 API token access key ID. |
| —                     | `CLICKCLACK_R2_SECRET_ACCESS_KEY`| unset       | R2 API token secret access key. |
| —                     | `CLICKCLACK_R2_ENDPOINT`         | derived     | Optional S3-compatible endpoint override for tests or non-standard R2 endpoints. |

## Config file

```jsonc
{
  "addr": ":8080",
  "data": "./data",
  "db": "sqlite:///var/lib/clickclack/clickclack.db",
  "uploads": "file:///var/lib/clickclack/uploads",
  "environment": "staging",
  "metrics_enabled": false,
  "dev_bootstrap": false,
  "public_url": "https://chat.example.com",
  "github_client_id": "Iv1.xxxxxxxxxxxx",
  "github_client_secret": "...",
  "github_allowed_org": "openclaw",
  "github_moderator_org": "openclaw",
  "pushover_api_token": "azGDORePK8gMaC0QOYAMyEEuzJnyUi",
  "r2_account_id": "91b59577e757131d68d55a471fe32aca",
  "r2_access_key_id": "...",
  "r2_secret_access_key": "..."
}
```

Pass with `--config /etc/clickclack/config.json`. Values from the file
override env vars; CLI flags override the file if explicitly set.

## DB URL

SQLite forms:

```
sqlite://./data/clickclack.db
./data/clickclack.db
```

Both end up at the same place — the `sqlite://` prefix is stripped. The
parent directory is created on open.

Postgres forms:

```
postgres://user:pass@host:5432/clickclack?sslmode=require
postgresql://user:pass@host:5432/clickclack?sslmode=require
```

`serve`, `migrate`, and admin commands all accept `--db` or
`CLICKCLACK_DB`. Postgres stores durable chat state in the external database.

## Upload storage

Local disk is the default:

```sh
CLICKCLACK_UPLOADS=file:///var/lib/clickclack/uploads
```

Cloudflare R2 uses the S3-compatible API:

```sh
CLICKCLACK_UPLOADS=r2://clickclack-uploads/prod
CLICKCLACK_R2_ACCOUNT_ID=91b59577e757131d68d55a471fe32aca
CLICKCLACK_R2_ACCESS_KEY_ID=...
CLICKCLACK_R2_SECRET_ACCESS_KEY=...
```

The database still stores upload metadata and auth visibility. The upload
backend stores the bytes and streams them back through `/api/uploads/{id}` after
the normal ClickClack permission checks.

## Disabling dev fallbacks

For non-local deployments:

```sh
clickclack serve \
  --dev-bootstrap=false \
  --config /etc/clickclack/config.json
```

Combine with real auth (CLI-created magic links or GitHub OAuth) so the
"first-user-in-DB" dev auth fallback never kicks in. In containers, this is
already the default; `CLICKCLACK_DEV_BOOTSTRAP=false` is only an explicit guard.
