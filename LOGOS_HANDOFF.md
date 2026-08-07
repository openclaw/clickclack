# PROJECT LOGOS — Handoff: What Was Built & The Correction

**Author:** RINCON (execution layer)
**Date:** 2026-08-07
**Purpose:** Handoff for Copilot to build the LOGOS companion application.
**Repo:** `CatabolicSolutions/clickclack` (fork of `openclaw/clickclack`)
**Branch:** `cognitive-os` (all work committed; push to fork: `git push fork cognitive-os`)

---

## 1. THE CORE MISALIGNMENT (READ FIRST)

**What Conor asked for:** Extract the **chat feature** from clickclack and lace it
into a **standalone companion application**. clickclack is the plumbing that
lives deep underneath; the companion app is the upspun asset/utility/experience.
The chat is the ONLY thing inherited from clickclack; everything else is new.

**What got built (the miss):** A heavily modified clickclack SPA (reskin +
panes + markers), plus a second app (`apps/logos`) served as a subpath
(`/logos/`) of the SAME clickclack deployment. That reads as "same program,
different coat of paint" — which is exactly what Conor rejected.

**The correction:** Build the companion application as a **separate, standalone
application** that:
- consumes clickclack's API + realtime as the chat substrate (chat = one
  embedded component, the only inherited piece)
- has its own shell, its own deployment, its own URL — NOT a subpath of the
  clickclack SPA, NOT a reskin of clickclack's UI
- everything around the chat (threads, memory, inspection, telemetry,
  personas, transforms, command palette) is native to the new app

---

## 2. WHAT ALREADY EXISTS (VERIFIED — REUSE, DON'T REBUILD)

### 2.1 The directive spec (authoritative)
- `LOGOS_SPEC.md` — Conor's full directive verbatim: conversational substrate,
  intent parser, persona engine, semantic threading, utility layer, memory
  graph, UI/UX, operational behavior, data schemas, behavior matrix, and the
  environment/visual design spec (§8: black/off-white operator console, 0px
  radius, dual fonts, 2px functional accents, split-blade inspection,
  keyboard-first).
- `LOGOS.md` — architecture (glass/brain split) + tracks + verification protocol.
- `LOGOS_STATUS.md` — build recap + deploy log.

### 2.2 The cognition service — DONE AND LIVE (the "brain")
`apps/cognition` — standalone TypeScript/Hono service, deployed on the droplet
as systemd `logos-cognition` (127.0.0.1:8787, auth-gated with shared secret).
Endpoints (all verified working with real DeepSeek output):
- `POST /analyze` — intent classification (ask/command/reflect/draft/clarify/
  explore) + persona suggestion + confidence + telemetry + clarification_question
- `POST /transform` — all 15 ops (summarize, expand, rewrite, counterargument,
  alternative_framing, diagram, checklist, plan, persona_rewrite, condense,
  extract, invert, simulate, draft, diagnose)
- `POST /threads/cluster` — embedding-based semantic clustering
- `POST /memory/anchors`, `GET /memory/query` — memory graph (local embeddings,
  cosine similarity, no external API key)
- `POST /respond` — adaptive response generator (cognitive mirroring: terse→
  concise, analytical→structured, brainstorming→exploratory; persona-driven;
  memory-cited) — **implemented by subagent track LOGOS-B; verify it landed**
- Local embeddings via transformers.js (all-MiniLM-L6-v2) — no OpenAI key needed

### 2.3 Message object schema — DONE AND LIVE
- `intent`, `persona`, `confidence`, `context_json`, `metadata_json`,
  `transform_history_json` columns on `messages` (SQLite + Postgres parity)
- `PATCH /messages/{id}/metadata` endpoint (additive, partial update)
- Server-side analyze-on-ingest: new messages get intent/persona/confidence
  automatically (async, non-blocking)

### 2.4 The LOGOS app shell — PARTIAL (reference, not final)
`apps/logos` — SvelteKit app started as the new surface. **Treat as component
reference, not as the finished product.** Contains:
- `src/styles/tokens.css` + `chassis.css` — exact §8 design tokens
- `src/lib/components/` — SemanticMargin, CommandPalette, SemanticThreadPane,
  ClarificationPrompt, MessageFrame (message anatomy), InspectorBlade (5-tab
  inspection), TelemetryRail, ChatStream (chat substrate component)
- `src/lib/clickclack/` — minimal API client (auth, workspaces, channels,
  messages, chatState store, polling→WS seam)
- `src/lib/cognition.ts` — cognition service client
- Deployed at `/logos/` on the clickclack origin — **this subpath mounting is
  part of what Conor rejected; the final app should be standalone**

### 2.5 Deployment topology (current, verified)
- Droplet `137.184.144.196` (SSH: `alfred-deploy`, key `~/.ssh/alfred_deploy_key`)
  - `clickclack.service` — clickclack API on :8090 (SQLite at /var/lib/clickclack)
  - `logos-cognition.service` — cognition service on :8787 (auth-gated,
    firewalled to Cloudflare IPs)
  - `logos-app.service` — LOGOS static server on :8788 (systemd, node)
- Cloudflare worker (`app.catabolicsolutions.com`, wrangler from repo root):
  - `/api/*` → droplet :8090
  - `/cognition/*` → droplet :8787 (strips prefix, injects auth token)
  - `/logos/*` → droplet :8788 (strips prefix)
- Credentials: `CREDENTIALS.MD/clickclack_cloudflare_env.txt` (CF API token,
  R2, Neon DB), `CREDENTIALS.MD/llm_keys_env.txt` (Anthropic/Kimi/DeepSeek),
  `CREDENTIALS.MD/agora_env.txt` (DeepSeek/OpenAI for the AGORA lane)

### 2.6 Tooling (ready)
- GitHub Copilot CLI v1.0.78 (npm -g `@github/copilot`), auth device flow
  available (`copilot login --device-code`) — a fresh code may be needed
- VS Code CLI v1.132.0 at `~/.local/bin/code` (tunnel/serve-web ready)
- No codex anywhere (explicitly removed — do not reintroduce)

---

## 3. WHAT COPI LOT SHOULD BUILD (THE CORRECT TARGET)

A **standalone companion application** (own app, own URL, own deploy) where:

1. **Chat is an embedded component** — the only thing inherited from
   clickclack. Uses clickclack's API (`/api/...` same-origin or CORS) +
   realtime; renders messages + composer. Reference: `apps/logos/src/lib/
   clickclack/` + `ChatStream.svelte`.
2. **Everything else is native to the companion app**:
   - operator console shell per LOGOS_SPEC §8 (tokens already written)
   - semantic thread sidebar (cognition /threads/cluster + /memory/query)
   - memory graph viewer (cognition /memory/*)
   - message inspection blades (telemetry/payload/stack — InspectorBlade.svelte)
   - command palette + keyboard-first (`:persona`, `:inspect`, j/k nav)
   - adaptive companion replies (cognition /respond)
3. **Deployed independently** — NOT a subpath of app.catabolicsolutions.com.
   Options: own subdomain (e.g. logos.catabolicsolutions.com) via the same CF
   account, or its own container/static host. Chat API calls either same-origin
   through a worker proxy or direct with CORS + cookie/bearer auth.

**Decisions Copilot should make (with Conor):**
- App framework: SvelteKit (matches repo + existing components) vs other
- Deploy target: CF worker/static vs droplet static server vs container
- Auth: reuse clickclack GitHub OAuth (cookie, same-origin) vs standalone auth

---

## 4. VERIFICATION NOTES / GOTCHAS

- `pnpm build` at repo root builds web + embeds into Go binary
  (`apps/api/internal/webassets/dist`) — the embed step is `pnpm embed:webassets`
- Worker route changes require `wrangler deploy` from repo root with CF creds
  sourced from `CREDENTIALS.MD/clickclack_cloudflare_env.txt`
- Cognition service rebuild: `cd apps/cognition && npm run build` → ship
  `dist/index.js` to droplet `/opt/logos-cognition/` → `systemctl restart
  logos-cognition`
- LOGOS static: `cd apps/logos && npm run build` → ship `dist/` to droplet
  `/opt/logos-app/dist`
- OAuth return-path fix is in the Go API (`github.go`): `?return_to=` is
  honored via cookie so sign-in lands back in the app that started it
- The `moonshot:default` OpenClaw auth profile has a cooldown until
  2026-08-07T08:19Z (billing-related); `moonshot:orion` is the live Kimi lane

---

## 5. PROVENANCE

- All claims above: verified in-repo on branch `cognitive-os` (commits
  e13899a → ff868f5) and/or verified live against the deployed services on
  2026-08-07.
- The three subagent tracks (LOGOS-A interaction, LOGOS-B /respond,
  LOGOS-C realtime) were running at handoff time — verify their work landed
  before building on top of it (`git log --oneline -10`, check
  `apps/cognition/src/index.ts` for /respond).
