# Composer Runtime Controls — Independent Data Plane (no ClawCanvas)

Status: PR #27 implementation note (Chisel, 2026-06-16)
Scope: model picker + context meter (+ the two-bar composer they live in) for clickclack,
sourced **entirely apart from ClawCanvas**.

## Decision

ragesaq: "we need to build this apart from anything clawcanvas provides."

clickglass gets its runtime facts by proxying ClawCanvas
(`/api/live/channels/:id/runtime` → `CLICKGLASS_IRONCANVAS_BASE`). clickclack will **not**.
clickclack owns its own runtime record, stored in its own SQLite, fed by the agent-bridge
reading the **gateway directly**. No `/api/live`, no clawcanvas base, no recoupling.

## The contract (app-agnostic)

The frontend stores (`channel-runtime.ts`) consume a flat `RuntimeRecord`. ClawCanvas is just
*a* producer of this shape, not part of it. The full field set the picker + meter read:

| Field | Meaning | Independent source |
|---|---|---|
| `default_model` | agent's configured model | gateway session/agent config (bridge reads at connect) |
| `default_thinking` | agent's configured thinking | gateway session/agent config |
| `model` | bridge-owned effective/current model | gateway session state |
| `thinking` | bridge-owned effective/current thinking | gateway session state |
| `override_model` | picker-owned desired next-turn model override | clickclack channel override row |
| `override_thinking` | picker-owned desired next-turn thinking override | clickclack channel override row |
| `context_used` | tokens in the live context window | gateway context accounting (the number `session_status` prints) |
| `context_limit` | model context window size | gateway context accounting |
| `cache_hit_pct` | optional composition cache rate | gateway/HyperMem diagnostics (optional, may be null) |
| `context_breakdown[]` | optional per-slot token breakdown | gateway diagnostics (optional, may be null) |

`pct` is computed client-side (`used/limit`), so the meter only needs `used`+`limit` to be truthful.

## Why every field is reachable without ClawCanvas

The agent-bridge already authenticates to the gateway as an **operator-scoped WS client**
(`client.id="gateway-client"`, `mode="backend"`, `role="operator"`, scopes
`operator.read`/`operator.write`). That is the same privilege level that powers `session_status`.

- **model / thinking / defaults / overrides** — session config facts the operator client can read
  (connect handshake + session state), and that already ride on agent events.
- **context_used / context_limit** — the gateway *computes* these per session; that is exactly what
  `session_status` renders ("Context: 100k/1.0m (10%)"). The operator client can read the same
  accounting. This is the only field that is a genuine gateway fact rather than config, and it is
  reachable today without clawcanvas.

So the meter ships **real**, not estimated. If a future gateway build exposes a cleaner
per-session usage event, the bridge swaps its source with no frontend change.

## Data plane

```
gateway (source of truth: model, thinking, context used/limit)
   │  operator WS (already connected for activity streaming)
   ▼
agent-bridge  ── stamps a RuntimeSnapshot per channel/session ──┐
                                                                 ▼
clickclack Go API  PUT /api/channels/{id}/runtime   (bot token, agent_activity:write)
   │  upsert into channel_runtime (own SQLite)
   ▼
clickclack Go API  GET /api/channels/{id}/runtime   (session auth)
   │  returns the flat RuntimeRecord
   ▼
frontend stores (channel-runtime.ts) → ComposerModelPicker + ComposerContextMeter
```

Override writes (picker changes model/thinking) flow the reverse way:
`PATCH /api/channels/{id}/runtime` (session auth + `messages:write`) → clickclack stores the desired override →
bridge applies it to the gateway session on the next turn. This channel-scoped runtime override is product-approved for PR #27: ordinary channel writers can change the channel's next-turn runtime override, but they cannot forge bridge/effective runtime facts. No clawcanvas model-proxy.

## Increments

1. **Backbone (clickclack only):** migration `channel_runtime`, store upsert/get, 
   `GET`/`PUT`/`PATCH /api/channels/{id}/runtime`. Bridge PUT is bot-scoped with
   `agent_activity:write`; picker PATCH is session-scoped with `messages:write`.
2. **Bridge stamp:** operator WS client reads gateway model/thinking/context-util and POSTs
   the snapshot. Applies session overrides the picker requests.
3. **Frontend port:** `channel-runtime.ts` + `channel-runtime-store.svelte.ts` +
   `live-channels.svelte.ts` (retargeted to clickclack's own `/runtime`), `ComposerModelPicker`,
   `ComposerContextMeter`, two-bar `ChatComposer`. Pointed at clickclack endpoints only.
4. **Deploy + verify** against real runtime rows. Demo seed data is intentionally removed; empty channels render default/unknown instead of fabricated model/context values.

## Non-goals

- No `/api/live` proxy, no `CLICKGLASS_IRONCANVAS_BASE`, no clawcanvas runtime endpoint.
- Context meter does not depend on ClawCanvas exposing a runtime endpoint.


## Frontend empty-state rule

The frontend does not seed demo runtime data. On channel switch, it clears the runtime store before fetching the selected channel's record, then applies only the response for the still-selected channel. Override-only records are displayable because `override_model`/`override_thinking` are real operator choices even before the bridge stamps effective/context fields. Empty records render default/unknown chrome; transient fetch failures do not preserve another channel's state.

The composer context meter renders only when the channel reports context-window accounting (`context_used`/`context_limit`). Channels with no runtime data or an override-only row have no context numbers and show no meter pill, rather than a meaningless `?/?` placeholder. This also guarantees the meter tears down on an in-app channel switch to a channel without context, so the previous channel's pill cannot linger.

### API-boundary validation

The picker `PATCH` validates its override fields before persisting: `thinking` must be one of `off|minimal|low|medium|high|xhigh|adaptive` (empty clears it) and `model` is length-bounded (≤256 chars, no control characters). A channel member cannot persist an arbitrary or oversized string that the bridge would later apply to a gateway session. The bridge `PUT` (bot token + `agent_activity:write`) carries gateway-sourced values and is not constrained by this picker allowlist.
