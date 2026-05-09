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
| `--config`            | —                                | unset       | JSON config file. |
| `--dev-bootstrap`     | `CLICKCLACK_DEV_BOOTSTRAP`       | `true`      | `serve` only. Creates a default user/workspace/channel and enables local dev auth fallbacks. |
| —                     | `CLICKCLACK_PUBLIC_URL`          | unset       | External URL. Used to build the GitHub OAuth callback. |
| —                     | `CLICKCLACK_GITHUB_CLIENT_ID`    | unset       | GitHub OAuth app client ID. |
| —                     | `CLICKCLACK_GITHUB_CLIENT_SECRET`| unset       | GitHub OAuth app client secret. |
| —                     | `CLICKCLACK_GITHUB_ALLOWED_ORG`  | unset       | Optional GitHub org login gate. Requires `read:org` scope. |
| —                     | `CLICKCLACK_PUSHOVER_API_TOKEN`  | unset       | Pushover application API token. Users still opt in with their own Pushover user key in account settings. |

## Config file

```jsonc
{
  "addr": ":8080",
  "data": "./data",
  "db": "sqlite:///var/lib/clickclack/clickclack.db",
  "dev_bootstrap": false,
  "public_url": "https://chat.example.com",
  "github_client_id": "Iv1.xxxxxxxxxxxx",
  "github_client_secret": "...",
  "github_allowed_org": "openclaw",
  "pushover_api_token": "azGDORePK8gMaC0QOYAMyEEuzJnyUi"
}
```

Pass with `--config /etc/clickclack/config.json`. Values from the file
override env vars; CLI flags override the file if explicitly set.

## DB URL

Two forms are accepted:

```
sqlite://./data/clickclack.db
./data/clickclack.db
```

Both end up at the same place — the `sqlite://` prefix is stripped. The
parent directory is created on open. Postgres is planned and will live behind
the same store interface.

## Disabling dev fallbacks

For non-local deployments:

```sh
clickclack serve \
  --dev-bootstrap=false \
  --config /etc/clickclack/config.json
```

Combine with real auth (magic links or GitHub OAuth) so the
"first-user-in-DB" fallback in `currentUser` never kicks in. In containers,
`CLICKCLACK_DEV_BOOTSTRAP=false` is the easiest way to enforce the same mode.
