# PROJECT LOGOS — COPILOT HANDOFF & DIRECTIVE SPEC

**Prepared:** 2026-08-07 01:58 MDT (RINCON, for Conor)
**Repo:** `CatabolicSolutions/clickclack` (fork of `openclaw/clickclack`)
**Branch:** `cognitive-os`
**Owner:** Conor Ross
**Authoritative docs (READ FIRST, in order):**
1. `LOGOS.md` — architecture + tracks + verification protocol
2. `LOGOS_SPEC.md` — full product directive (8 sections, source of truth)
3. `LOGOS_STATUS.md` — build recap, deploy log, forward plan
4. This file — the clarification + what to do next

---

## 1. THE ACTUAL ASK (clarification — read carefully)

**We are building a STANDALONE communications application — PROJECT LOGOS — that uses ClickClack as its communication connection/transport layer. We are NOT reskinning the ClickClack web app itself.**

- **ClickClack = the communication connection (substrate):** `apps/api` (Go server, SQLite/Postgres, WebSockets, channels, auth, message transport) + `apps/web` (its stock SPA, which stays stock as the underlying transport's UI).
- **LOGOS = the standalone app we are building:** `apps/logos` (the new SPA — operator console, message objects, semantic surfaces) + `apps/cognition` (the brain — intent/persona/transforms/semantic threads/memory).
- **Architecture (locked):** THE GLASS (logos SPA + clickclack API) ◄─HTTP─► THE BRAIN (cognition service). ClickClack stays fast and dumb; the brain is a separate swappable service.
- **Every message is a data object:** `{content, intent, persona, context, thread_id, confidence, metadata, transform_history}` — persisted via additive columns, rendered with state markers, transformable inline, linkable to memory nodes and semantic threads.

**Why this matters:** prior work touched the ClickClack web app's chrome (reskin/chassis) and left it "semi broken" in places. The target surface is the STANDALONE logos app. ClickClack's own UI should be left alone / restored, not re-themed.

---

## 2. REPO MAP

| Path | What it is | Status |
|---|---|---|
| `apps/api` | ClickClack Go server (transport/connection) | stock upstream + T2 schema additions |
| `apps/web` | ClickClack Svelte SPA (stock transport UI) | rollback commit applied to fix broken viewport (`5e999ad`); do not re-theme |
| `apps/logos` | **THE STANDALONE APP** (Vite/Svelte SPA, operator console) | active build — L1-L3 integrated (`fe3146c`) |
| `apps/cognition` | **THE BRAIN** (Hono/TS service) | scaffold + semantic layer live |
| `apps/desktop` | desktop shell | upstream |

---

## 3. WHAT IS COMPLETED (verified, committed on `cognitive-os`)

**T0 — Foundation ✅** (`e13899a`, `cfac4a4`)
- Architecture decision (glass/brain split), `LOGOS.md` plan, `LOGOS_SPEC.md` full directive, feature branch.

**T1 — Logos SPA reskin ✅** (`1a6df59`)
- Design tokens (`tokens.css`), flat message rows (no bubbles), `CognitiveMarkers.svelte` (intent band/persona tag/confidence/thread/execution — absent-state until cognition data), `MessageUtilities.svelte` (hover utility bar → `cognition.ts` stub, `VITE_COGNITION_URL`).
- `npm run typecheck` + `npm run build` pass.

**T2 — Message object schema in API ✅** (`1a6df59`)
- Migration `0041_cognitive_os_message_fields.sql` (sqlite + postgres): `intent, persona, confidence, context_json, metadata_json, transform_history_json`.
- `Message` struct + `UpdateMessageMetadataInput`; sqlite + postgres store impls.
- `PATCH /messages/{id}/metadata` — additive, partial, access-controlled.
- OpenAPI updated; `go build ./...` + full `go test ./...` pass.

**T3 — Cognition service scaffold ✅** (`1ad3bdc`)
- `apps/cognition` Hono/TS: `/healthz`, `/analyze`, `/transform`, `/threads/cluster`, `/memory/anchors|query|list` — smoke-tested live.
- Message-object types; intent/persona/transform-op unions (spec 6.2); `LlmClient` interface + `StubLlmClient`; `JsonFileStore`; validation + 404s. tsc clean.

**§8 Upgraded-app chassis ✅** (deployed 2026-08-07)
- v2 design tokens (pure black `#000`, off-white `#F4F4F0`, charcoal `#1A1A1A`, functional 2px intent bands only), Inter + JetBrains Mono.
- Chassis A: fixed tiled grid, semantic margin, `CommandPalette` (Cmd+K //), `lib/ui.ts` stores, 100–150ms linear motion.
- Chassis B: message anatomy (2px intent edge band, mono metadata header, action rail, `InspectorBlade` telemetry/memory/logprobs/payload/stack), Alt diagnostic mode.

**Semantic layer ✅** (deployed 2026-08-07 01:0x)
- **Local embeddings** (transformers.js `all-MiniLM-L6-v2`) — loads ~2s, zero API key. Replaced dead OpenAI key.
- `/memory/query` cosine-ranked (measured 0.483, no substring), `/threads/cluster` real (threshold tuned 0.55→0.25).
- Telemetry on analyze/transform (latency_ms, tokens, model, execution_stack, intent_vector_score, memory_citations).
- `SemanticThreadPane` (THREADS|MEMORY), clarification prompts (`[ASK]/[DISMISS]`), InspectorBlade telemetry wired, cluster contract fixed.

---

## 4. WHAT IS DEPLOYED (as of 2026-08-07 01:0x)

| Component | Where | Evidence |
|---|---|---|
| Droplet binary (T1 reskin + T2 schema) | `/opt/clickclack/clickclack` (backup `.bak-20260806`) | service active, root HTTP 200 |
| SQLite migration 0041 | applied on messages | 6 cognitive columns present |
| Cognition service | systemd `logos-cognition` on :8787 | healthz ok; real DeepSeek analyze/transform |
| Firewall | 8787 → Cloudflare IP ranges only | locked |
| Cloudflare worker | `app.catabolicsolutions.com` — `/cognition/*`→:8787, `/api/*`→:8090 | worker b27103ca; container (Neon) rebuilt, migration 0042 |
| Public checks | SPA 200 + served CSS hash matches reskinned build; `/cognition/analyze` real LLM output | green |

---

## 5. WHAT IS SEMI-BROKEN / DEGRADED / PENDING

1. **ClickClack stock web chrome** — prior reskin/chassis work touched it; rollback commit fixed the viewport, but treat `apps/web` as transport UI only (restore/leave stock). The STANDALONE surface is `apps/logos`.
2. **T4 integration NOT done** — logos SPA → cognition wiring (`VITE_COGNITION_URL`): markers still render as absent states; hover utilities are stubs.
3. **T3 Phase B intelligence** — scaffolded but real LLM classification/persona/transform prompt-systems, embeddings clustering refinement, memory-graph semantics = handoff lane.
4. **Copilot auth pending** — device flow was pending at deploy; needed for the designated frontier lane.
5. **Logprobs n/a** — DeepSeek API doesn't return them; InspectorBlade renders n/a (never faked).
6. **T5 telemetry canvas** — live logprobs/vector distance canvas not built.

---

## 6. DIRECTIVE SPEC FOR COPILOT — WHAT TO DO NEXT

**Priority order (do not skip verification):**

1. **Fix the "semi broken" ClickClack web surface (quick):**
   - Confirm `apps/web` builds and the stock app renders cleanly. The standalone logos app (`apps/logos`) is the product surface — do NOT re-apply chassis/reskin themes to `apps/web`.
2. **T4 — Wire logos SPA → cognition service:**
   - Set `VITE_COGNITION_URL`; connect `cognition.ts` stub to `/cognition/analyze|transform` (via Cloudflare worker path).
   - Analyze-on-ingest or on-demand → `PATCH /messages/{id}/metadata`.
   - Populate markers (intent band, persona, confidence, thread, execution) from real cognition data; hover utilities live.
3. **T3 Phase B — intelligence (the brain's real work):**
   - Intent parser (six buckets: ask/command/reflect/draft/clarify/explore) — real LLM classification.
   - Persona engine (five personas: operator/analyst/creative/socratic/archivist).
   - Transformer ops (summarize/expand/rewrite/counterargument/alternative_framing/diagram/checklist/plan/persona_rewrite/condense/extract/invert/simulate/draft/diagnose).
   - Semantic threads: embeddings + clustering + cross-thread retrieval (local all-MiniLM already wired).
   - Memory anchors: pin message → memory node; `/memory/query` (already cosine-ranked).
4. **T5 — Telemetry & inspection (after T3/T4):**
   - Split-blade inspection (vector distance, confidence, logprobs when provider supports), live telemetry canvas.
5. **Restore/verify ClickClack stock** as the communication connection — channels, auth, WS, message transport all stay upstream-stock except the additive T2 metadata fields.

**Cost rules (Conor):** No high-cost/frontier models unless necessary. Default lanes: deepseek-v4-pro subagents; Copilot CLI / VS Code CLI (`code` at `~/.local/bin/code`) for T3+ handoffs. NO Codex (removed, never use).

**Verification protocol (non-negotiable):**
- `cd apps/web && npm run build && npm run typecheck` passes
- `cd apps/api && go build ./... && go test ./...` passes
- `cd apps/logos && npm run build && npm run typecheck` passes
- `cd apps/cognition && npm run typecheck` passes
- Migrations additive; existing API contract backward compatible
- No `git add -A` — stage only files you changed
- Commit messages: `logos: <track> — <what>`

**Deploy (existing path):**
1. `cd apps/web && npm run build` (transport UI stays stock)
2. `cd apps/logos && npm run build` (standalone app — verify which binary embeds it)
3. `cd apps/api && go build ./cmd/clickclack`
4. scp binary → droplet `/opt/clickclack/clickclack` (backup first)
5. `sudo systemctl restart clickclack.service` (+ `logos-cognition` if changed)
6. Cloudflare worker already proxies `app.catabolicsolutions.com` → droplet

**Open questions for Conor (only he can decide):**
- Should the standalone logos app be served at a new subdomain/path (e.g., `logos.catabolicsolutions.com`) separate from the clickclack transport UI?
- Keep DeepSeek as the cognition LLM provider, or switch to OpenAI for logprobs support?

---

*RINCON — 2026-08-07 01:58 MDT. All facts above verified from the repo tree, commit history, and deploy logs at time of writing.*
