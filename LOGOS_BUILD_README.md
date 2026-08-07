# PROJECT LOGOS — Build README (what was done to clickclack so far)

**Date:** 2026-08-07
**Repo:** `CatabolicSolutions/clickclack` (fork of `openclaw/clickclack`)
**Branch:** `cognitive-os` — HEAD `894c326`, all work committed + pushed to fork
**Owner:** Conor Ross / RINCON
**Purpose:** Hand-off record. What exists, what was changed, what is live — so a
fresh builder (Copilot) can pick up without re-deriving state.

---

## 1. The intent (restated)

ClickClack is **plumbing** — it lives deep underneath. The goal is a **standalone
companion application** ("Project LOGOS") that:
- **extracts the chat feature** from clickclack and laces it in as one embedded
  component,
- builds everything else (threads, memory, inspection, telemetry, personas,
  transforms, command palette) **natively in the new app**,
- has **its own shape/design/UX/interface, own URL, own deployment** — NOT a
  reskin of clickclack's UI, NOT a subpath of its origin.

---

## 2. What was built on the `cognitive-os` branch (commits, newest first)

| Commit | What |
|---|---|
| `894c326` | logos: root-relative build for standalone origin (drop `/logos` base) |
| `3888f26` | logos: standalone origin — `logos.catabolicsolutions.com` serves LOGOS at root with same-origin `/api` + `/cognition` (worker host-aware routing + custom domain) |
| `ca641b8` | logos: T2-F3 — fix OpenAPI duplicate `patch:` key; metadata PATCH as own path; regenerated SDK types |
| `57fad68` | logos: T2-F2 — E2E PATCH round-trip test **caught a real bug**: sqlite read path dropped all 6 cognitive fields on read-back; fixed `messageSelect()`/`scanMessage()`; migration tests pre-apply 0041 |
| `df6460a` | logos: fixed stale asset-boundary test (PWA service-worker/manifest are now legit embedded assets) |
| `357c3ed` | logos: hygiene — untracked `apps/logos/.svelte-kit` build artifacts |
| `b79dc66` | logos: total scope master record (`LOGOS_SCOPE.md`) |
| `cca1f16` | logos: recap + T2 finish / T3 start decision doc (CLIs verified) |
| `c7b096a` | logos: Phase B/C build block for Copilot/VSCode |
| `e843997` | logos: realtime seam details (WS path/protocol/fallback) |
| `389b54f` | logos: landed in-flight tracks — interaction layer + adaptive `/respond` + realtime seam |
| `0946007` | logos: handoff doc + correction advisory |
| `fe3146c` / `cf2bbc4` | logos: new application `apps/logos` — chat substrate + semantic surfaces + inspection (L1-L3) |
| earlier | semantic layer (local embeddings, clustering, telemetry), upgraded chassis (spec §8), v2 tokens |

---

## 3. What is live right now (verified 08-07)

- **`apps/logos`** — SvelteKit standalone app (adapter-static, root-relative):
  - `src/styles/tokens.css` + `chassis.css` — exact spec §8 design system
    (#000000 / #F4F4F0 / #1A1A1A, 0px radius, Inter + JetBrains Mono, 100-150ms
    linear motion, 2px functional accents)
  - `src/lib/components/` — MessageFrame (intent band, mono metadata header,
    inline action rail, CONF-click split-blade), SemanticMargin (grid marks +
    line counters), CommandPalette (Cmd+K, `/`, `:persona`, `:inspect`),
    SemanticThreadPane (THREADS | MEMORY tabs), InspectorBlade (TELEMETRY |
    MEMORY | LOGPROBS | PAYLOAD | STACK), ClarificationPrompt, TelemetryRail,
    ChatStream (embedded chat component)
  - `src/lib/clickclack/` — minimal API client (cookie auth, workspaces,
    channels, messages, chatState, WS realtime with poll fallback)
  - `src/lib/cognition.ts` — typed client for the cognition service
- **`apps/cognition`** — standalone brain service (Hono/TS), LIVE on droplet
  (`logos-cognition` :8787, auth-gated, Cloudflare-only):
  - `/analyze` (intent/persona/confidence/telemetry/clarification),
    `/transform` (15 ops), `/respond` (adaptive, memory-cited),
    `/threads/cluster` (local embeddings, all-MiniLM), `/memory/anchors|query|list`
  - Local embeddings via transformers.js — no OpenAI key needed
- **API (clickclack plumbing)**: T2 message-object schema live — `intent`,
  `persona`, `confidence`, `context_json`, `metadata_json`,
  `transform_history_json` on `messages` (sqlite + Neon postgres, verified
  6/6 columns), `PATCH /messages/{id}/metadata`, server-side analyze-on-ingest
- **Deployments**: clickclack binary on droplet (:8090, backup kept),
  cognition service :8787, logos static :8788, Cloudflare worker version
  `473db384` with custom domains `app.catabolicsolutions.com` +
  `logos.catabolicsolutions.com`

---

## 4. Verification state

- `cd apps/api && go build ./... && go test ./...` — **all green**
  (incl. new E2E metadata round-trip; suite was failing before the read-path fix)
- `cd apps/logos && npm run build` — passes (typecheck + build)
- `cd apps/cognition && npm run build` — passes (tsc + esbuild)
- Live checks: `https://logos.catabolicsolutions.com/` serves LOGOS (200);
  `/cognition/healthz` ok; `/cognition/analyze` returns real DeepSeek output;
  `/api/me` correctly requires auth

---

## 5. Known gaps / honesty notes

1. **Standalone deployment identity exists but the app still needs finishing**
   — the standalone origin + worker routing + root-relative build are in place;
   the companion UX build-out (what Conor actually wants) is the remaining work.
2. `.svelte-kit` artifacts untracked (fixed); keep them out of commits.
3. Logprobs: DeepSeek API doesn't return them — InspectorBlade shows n/a,
   never faked. Use intent_vector_score, latency, tokens, citations instead.
4. OpenAI embeddings key is dead (401) — local all-MiniLM embeddings replace it.
5. Copilot CLI auth was attempted via device flow during this session; the
   user declined to complete it. A fresh `copilot login --device-code` will be
   needed if Copilot CLI is used for the build hand-off.
