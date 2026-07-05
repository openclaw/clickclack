---
read_when:
  - setting up a fresh checkout
  - changing the gate (lint/typecheck/test/build/coverage)
  - adding a new package or tool to the monorepo
---

# Development

The repo is a Go module plus a pnpm workspace. The Go binary embeds the
built SPA, so a full local build runs both toolchains.

## Prerequisites

- Go (matching `go.mod`).
- pnpm 11 (auto-managed via `corepack`).
- TypeScript runs via `tsgo` from `@typescript/native-preview` — installed
  through pnpm.
- Lint/format use `oxlint` and `oxfmt` — installed through pnpm.

## First run

```sh
pnpm install
pnpm build                                          # builds SPA + SDK and copies dist into apps/api
go run ./apps/api/cmd/clickclack serve
open http://localhost:8080
```

The dev fallback creates `Local Captain` as the first user, a `ClickClack`
workspace, and a `general` channel, so the SPA loads into a working state on
first hit.

## Two-process dev loop

```sh
# terminal 1
pnpm dev:api                                        # go run ... serve --dev-bootstrap=true

# terminal 2
pnpm dev:web                                        # vite dev server with API proxy
```

The Vite dev server proxies `/api` and `/api/realtime/ws` to `localhost:8080`.

## Scripts

| Command                | What it does |
|------------------------|--------------|
| `pnpm build`           | Builds the Svelte app and the SDK, then embeds `apps/web/dist` into `apps/api/internal/webassets/dist`. |
| `pnpm build:web`       | Builds and normalizes the Svelte app without touching embedded Go assets. |
| `pnpm build:sdk`       | Builds the TypeScript SDK. |
| `pnpm build:desktop`   | Bundles the Electron main process, preloads, and settings renderer. |
| `pnpm check`           | Full local gate: `pnpm test`, root/workspace `tsgo`, `oxlint`, and format checks. |
| `pnpm coverage`        | Go tests with coverage; fails under 85% line coverage. |
| `pnpm dev:api`         | `go run ./apps/api/cmd/clickclack serve --dev-bootstrap=true`. |
| `pnpm dev:web`         | `vite dev` for the SPA. |
| `pnpm dev:desktop`     | Builds and starts the Electron client against its configured server. |
| `pnpm fmt`             | `gofmt` + `oxfmt` over Go and TS/Svelte. |
| `pnpm fmt:check`       | CI-compatible formatting check with `gofmt -l` and `oxfmt --check`. |
| `pnpm lint`            | `oxlint` over web, SDK, examples, and tests. |
| `goreleaser release --snapshot --clean` | Local release smoke test for all configured OS/arch targets. |
| `pnpm typecheck`       | `tsgo --noEmit -p tsconfig.json` for root Playwright config/tests. |
| `pnpm test`            | Builds the web app and SDK, then runs Go tests against those fresh web assets in a temp copy without rewriting tracked embedded assets. |
| `pnpm test:e2e`        | Playwright suite in `tests/e2e`. |
| `pnpm test:desktop`    | Tests desktop URL, deep-link, notification, settings, and badge contracts. |

`pnpm build` uses `CLICKCLACK_WEB_VERSION=dev` by default. That keeps repeated
local builds deterministic while still allowing real source changes to update
content-hashed assets. Release and Docker builds should set
`CLICKCLACK_WEB_VERSION` to the commit or tag being shipped.

## Layout

```
apps/
  api/                  # Go backend, single-binary entrypoint
    cmd/clickclack/     # CLI main
    internal/
      auth/             # placeholder
      config/           # flag/env/file resolution
      httpapi/          # chi router, handlers, auth resolution
      realtime/         # in-process pub/sub hub
      store/            # store interface + types
        sqlite/         # SQLite implementation, migrations, backup, export
        postgres/       # Postgres implementation, migrations, export
      webassets/        # go:embed for the built SPA
  desktop/              # Electron shell, platform assets, settings, packaging
  web/                  # Svelte 5 SPA
packages/
  protocol/             # OpenAPI spec, source of truth for the wire shape
  sdk-ts/               # TypeScript SDK (generated types + friendly wrapper)
examples/
  bot-ts/               # SDK usage example
infra/
  migrations/sqlite     # mirror of embedded SQLite migrations for tooling
  migrations/postgres   # mirror of embedded Postgres migrations for tooling
tests/
  e2e/                  # Playwright tests
docs/                   # this directory
```

## Adding a feature

1. Update `packages/protocol/openapi.yaml` first when the wire shape
   changes. It is the contract.
2. Add the store method on `apps/api/internal/store/types.go` and implement
   it in both `apps/api/internal/store/sqlite` and
   `apps/api/internal/store/postgres` when the feature touches durable state.
3. Wire the handler in `apps/api/internal/httpapi`.
4. Update the SDK in `packages/sdk-ts/src/index.ts` so TS clients have a
   typed surface.
5. Update or add a `docs/features/<thing>.md`.
6. Run `pnpm check` and `pnpm coverage`.

## Testing

- `apps/api/internal/...` is the bulk of the test suite. Coverage gate is
  85%.
- `tests/e2e/chat.spec.ts` exercises the SPA end-to-end via Playwright.
- The SDK has no test target yet — the bot example is the smoke test.

## Coding rules

- IDs are sortable ULID-style with semantic prefixes (`usr_`, `wsp_`, `chn_`,
  `msg_`, `evt_`, `upl_`, `idn_`).
- Keep transactions short. Outbox events are inserted in the same tx as the
  durable write that produced them.
- Keep SQL behind the store interface. Dialect-specific SQL belongs in the
  SQLite/Postgres store packages, not in HTTP handlers.
- Use `sqlc` for typed SQL. Edit schema/query files, then run
  `pnpm generate:sqlc`; do not hand-maintain generated `storedb` code.
- TypeScript: no Svelte imports in `packages/sdk-ts`. The SDK must stay
  framework-neutral.
