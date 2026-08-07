# PROJECT LOGOS — Correction Advisement (for Copilot)

**Date:** 2026-08-07
**For:** Copilot (or any builder taking over)
**From:** Conor Ross (owner) via RINCON
**Read first:** `LOGOS_BUILD_README.md` (what exists), `LOGOS_SPEC.md` (full
directive + §8 visual language), then this document.

---

## 1. The core correction (read this twice)

**What Conor asked for:** extract the **chat feature** from clickclack and lace
it into a **standalone companion application**. clickclack is the plumbing that
lives deep underneath; the companion app is the upspun asset/utility/experience.
The chat is the ONLY thing inherited from clickclack; everything else is new.

**What went wrong (the miss):** work kept modifying the clickclack SPA/API and
mounting a second app as a subpath of the same deployment. That reads as "same
program, different coat of paint" — which is what Conor rejected. He has said
this explicitly more than once:
- The upgraded app inherently means **different shape/design/UX/interface**.
- It is NOT a reskin of clickclack's UI.
- It is NOT a subpath of clickclack's origin.

**The correct target:**
- A **separate, standalone application** with **its own shell, its own
  deployment, its own URL** (e.g. `logos.catabolicsolutions.com` — DNS/custom
  domain already provisioned).
- **Chat = one embedded component** — the only inherited piece. Uses
  clickclack's API + realtime (cookie auth, WS, REST).
- **Everything around the chat is native to the new app**: operator console
  shell (§8), semantic thread sidebar, memory graph viewer, message inspection
  blades, command palette + keyboard-first nav, adaptive companion replies
  (`/respond`).

---

## 2. Do NOT do these

- ❌ Do not reskin the clickclack SPA (`apps/web`) in place.
- ❌ Do not keep mounting LOGOS as `/logos/` subpath of `app.catabolicsolutions.com`
  as the final surface. (That mount was the rejected pattern. The standalone
  origin `logos.catabolicsolutions.com` is the target.)
- ❌ Do not add new features to the clickclack Go API unless they are plumbing
  the companion needs (message schema, metadata, realtime — already done).
- ❌ Do not use Codex. No codex lanes, ever.
- ❌ Do not spend frontier/high-cost model tokens casually. Default to the
  existing DeepSeek wiring; reserve frontier for design review only.

---

## 3. What already exists — REUSE, DON'T REBUILD (verified in-tree + live)

### The design system (spec §8) — already written
`apps/logos/src/styles/tokens.css` + `chassis.css`:
- Monochromatic palette (#000000 / #F4F4F0 / #1A1A1A), 2px functional accents
  (Phosphor Green #00FF66, Amber #FFB000, Cobalt #0088FF)
- 0px border radius, rigid tiled grid, dual fonts (Inter + JetBrains Mono)
- 100-150ms linear motion; keyboard-first tokens

### Components — already written (in `apps/logos/src/lib/components/`)
- `MessageFrame.svelte` — intent edge band (2px, six intent colors), mono
  metadata header `[INTENT][PERSONA][CONF][THREAD][LATENCY]`, inline action rail
  (`> [XFORM] [CONDENSE] [EXPAND] [MEM-NODE] [REWRITE]`), CONF-click split-blade
- `SemanticMargin.svelte` — 1px grid marks + line counters
- `CommandPalette.svelte` — Cmd+K / `/`, `:persona`, `:inspect`, `:telemetry`,
  `:threads`, Escape
- `SemanticThreadPane.svelte` — THREADS | MEMORY tabs, CL-XX chips,
  cross-thread retrieval
- `InspectorBlade.svelte` — TELEMETRY | MEMORY | LOGPROBS | PAYLOAD | STACK
- `ClarificationPrompt.svelte`, `TelemetryRail.svelte`, `ChatStream.svelte`
- `src/lib/clickclack/` — API client (cookie auth), chatState, WS realtime with
  poll fallback
- `src/lib/cognition.ts` — typed client for the brain service
- `src/lib/ui.ts` — inspectMode (Alt diagnostic view), pane state stores

### The brain service — DONE AND LIVE
`apps/cognition` (Hono/TS) on droplet `logos-cognition` :8787 (auth-gated,
Cloudflare-only):
- `POST /analyze` — intent (ask/command/reflect/draft/clarify/explore) +
  persona suggestion + confidence + telemetry + clarification_question
- `POST /transform` — all 15 ops
- `POST /threads/cluster` — embedding-based semantic clustering (local
  all-MiniLM-L6-v2, no API key)
- `GET /memory/query`, `POST /memory/anchors`, `GET /memory/list`
- `POST /respond` — adaptive response generator (cognitive mirroring, persona,
  memory citations)
- Verified live through the worker (real DeepSeek output)

### The plumbing (clickclack) — message schema DONE
- `messages` table: intent, persona, confidence, context_json, metadata_json,
  transform_history_json (sqlite + Neon postgres, 6/6 verified)
- `PATCH /messages/{id}/metadata` (additive, partial)
- Server-side analyze-on-ingest (async, non-blocking)
- Realtime WS: `/api/realtime/ws?workspace_id=...` (cookie auth), tail bootstrap
  via `/api/realtime/events?...&include_tail=true`, reconnect backoff 1s→2s→10s
  poll + 60s WS retry

---

## 4. What to build (the actual work)

1. **Standalone companion shell** — take the §8 chassis + components and stand
   them up as the definitive LOGOS app at `logos.catabolicsolutions.com` with
   the embedded ChatStream as the chat substrate. Own look/UX — different shape
   and feel from clickclack's UI.
2. **Wire the surfaces** (SPA → cognition):
   - analyze-on-ingest / on-demand → populate intent band, persona tag,
     confidence, thread marker from real data
   - hover action rail live (transform ops hit `/transform`)
   - thread sidebar from `/threads/cluster`; memory links from `/memory/query`
   - adaptive `/respond` in the composer flow
   - inspector blades from message `metadata_json.telemetry`
3. **Deploy as its own surface** — worker already routes
   `logos.catabolicsolutions.com` → LOGOS static (:8788) with same-origin
   `/api` + `/cognition`; refine as needed.
4. **T2 finish items (optional but cheap):** Neon parity re-run, E2E PATCH
   round-trip (test already written), SDK regen (types already regenerated).

---

## 5. Deploy topology (current, verified)

- Droplet `137.184.144.196`:
  - `clickclack.service` — API :8090 (SQLite at /var/lib/clickclack)
  - `logos-cognition.service` — brain :8787 (auth-gated, firewalled to CF IPs)
  - `logos-app.service` — LOGOS static :8788
- Cloudflare worker (`app.catabolicsolutions.com` + `logos.catabolicsolutions.com`,
  wrangler from repo root):
  - `/api/*` → :8090 · `/cognition/*` → :8787 (strips prefix, injects token) ·
    logos origin root → :8788 (host-aware routing)
- Credentials: `CREDENTIALS.MD/clickclack_cloudflare_env.txt` (CF/R2/Neon),
  `CREDENTIALS.MD/llm_keys_env.txt` (DeepSeek/Anthropic/Kimi)

---

## 6. Build commands (verified)

- `cd apps/logos && npm run build` → dist/ (root-relative, adapter-static)
- `cd apps/cognition && npm run build` → dist/index.js
- `cd apps/api && go build ./... && go test ./...` (all green)
- Deploy: scp dist → droplet `/opt/logos-app/dist`, restart `logos-app`;
  cognition → `/opt/logos-cognition/index.js`, restart `logos-cognition`;
  worker → `npx wrangler deploy` from repo root (CF creds from env file)
- Commit format: `logos: <track> — <what>`; stage only changed files; push to
  fork `cognitive-os`

---

## 7. Success criteria (what "done" looks like)

1. `logos.catabolicsolutions.com` loads a **standalone app** that looks nothing
   like clickclack's UI — §8 console, keyboard-first, no bubbles/emojis/gradients.
2. Chat works inside it (messages in/out, realtime) via the embedded chat
   component — same account/session as clickclack.
3. Intent/persona/confidence markers populate from real cognition data.
4. Transforms, threads, memory, inspection all function from the companion UI.
5. `go test ./...` + `npm run build` (logos + cognition) green; deployed and
   verified live.

Then hand it back to Conor/RINCON for tweaks.
