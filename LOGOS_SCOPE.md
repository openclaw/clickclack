# PROJECT LOGOS — TOTAL SCOPE (master record)

**Status:** ACTIVE — authoritative project record (supersedes partial docs)
**Owner:** Conor Ross / RINCON
**Repo:** `CatabolicSolutions/clickclack` · Branch: `cognitive-os`
**Directive (verbatim):** `LOGOS_SPEC.md` · Plan/architecture: `LOGOS.md` · Status: `LOGOS_STATUS.md` · Handoff: `LOGOS_HANDOFF.md` · This doc: complete scope + slice plan

---

## 0. THE PROJECT IN ONE PARAGRAPH

Re-architect chat from a linear text log into an object-oriented cognitive
substrate: every message is a data object (intent, persona, confidence,
context, thread affiliation, transform history). A standalone companion
application ("the glass") renders those objects in a typewriter-minimal
operator console; a separate cognition service ("the brain") owns all
intelligence — intent parsing, persona adaptation, inline transforms,
semantic threading, memory graph, adaptive responses. clickclack remains the
dumb, fast plumbing underneath; LOGOS is the upspun asset.

**Hard correction (recorded 08-07):** LOGOS is a STANDALONE application with
its own URL/deploy. Chat is the ONLY inherited clickclack piece (embedded
component). NOT a reskin of clickclack's SPA, NOT a subpath mount. The
current `/logos/` subpath deployment is a dev sandbox, not the final surface.

---

## 1. OUTLINE — THE FULL DIRECTIVE (recorded from Conor, expanded into scope)

### 1.1 Conversational substrate (spec §1)
- Chat = dynamic cognitive space, not chronological log.
- Every message = intent-encoded unit, executable instruction, memory
  anchor, semantic node, adaptive rendering object.

### 1.2 Intelligence layer (spec §2)
- **Intent parser:** 6 buckets — ask, command, reflect, draft, clarify,
  explore. Classification drives tone/structure/depth.
- **Persona engine:** 5 personas — operator, analyst, creative, socratic,
  archivist. Explicit invocation + automated inference.
- **Semantic threading:** auto-cluster messages into semantic threads;
  merge/split/archive (manual + programmatic); cross-thread semantic
  retrieval; threads persist as living documents.

### 1.3 Utility layer (spec §3)
- **Inline transforms (8):** summarize, expand, counterargument,
  alternative_framing, diagram, checklist, plan, persona_rewrite.
- **Native conversational tools (7):** rewrite, condense, extract, invert,
  simulate, draft, diagnose.
- **Memory graph:** tracks preferences, tone, cognitive patterns, recurring
  topics, project contexts, entities/people, operational style. Queryable
  conversationally (patterns, evolution, last-N decisions).

### 1.4 UI/UX (spec §4)
- Black + off-white operator-grade; NO bubbles, rounded corners, emojis,
  gradients.
- Message state markers: intent color band, persona tag, confidence
  indicator, thread affiliation, execution marker.
- Non-modal inline utilities (hover/tap): transform, summarize, expand,
  thread link, memory link, persona switch.

### 1.5 Operational behavior (spec §5)
- Proactive clarification (ambiguity → prompt before processing).
- Cognitive mirroring (terse→dense, analytical→structured,
  brainstorming→exploratory).
- Conversation sculpting: merge streams, extract summaries, build outlines,
  detect contradictions, extract insights.

### 1.6 Schemas & modules (spec §6)
- Modules: intent parser, persona engine, semantic thread manager, message
  transformer, memory graph, inline utility toolkit, adaptive response
  generator.
- Message object schema: content, intent, persona, context, thread_id,
  confidence, metadata, transform_history (persisted: intent, persona,
  confidence, context_json, metadata_json, transform_history_json).

### 1.7 Environment & visual language (spec §7 + §8 — authoritative chassis)
- Palette: `#000000` bg, `#F4F4F0` type, `#1A1A1A` borders. Accents strictly
  functional 2px: Phosphor Green `#00FF66`, Amber `#FFB000`, Cobalt `#0088FF`.
- Zero decorative artifacts; 0px radius; rigid grid; dual fonts (Inter +
  JetBrains Mono).
- Intent edge bands: Ask `#D1D1D1`, Command `#FFB000`, Reflect `#4A5568`,
  Draft `#00FF66`, Clarify `#FF0055`, Explore `#0088FF`.
- Metadata header per message: `[INTENT] [PERSONA] [CONF] [THREAD_ID] [LATENCY]`.
- Inline action rail: `[XFORM] [CONDENSE] [EXPAND] [MEM-NODE] [REWRITE]`.
- Deep inspection: inspector blades (latency, tokens, intent vector score,
  memory citations, logprobs), Alt diagnostic mode (dashed vector lines,
  token probabilities), split-blade JSON payload dump.
- Motion: linear/step 100-150ms, no easing. Keyboard-first: Cmd+K / `/`
  command bar, vim-style nav, `:persona`, `:inspect`.

---

## 2. MODULE → TRACK MAP (what builds what)

| Module | Track | Status |
|---|---|---|
| Message object schema (data layer) | T2 (API) | ✅ core done, shipped |
| SPA rendering / console chassis | T1 (app) → T4 → T5 | 🔄 T1 done; standalone + T4/T5 pending |
| Intent parser | T3 (cognition) | 🔄 scaffold + Phase B core; hardening pending |
| Persona engine | T3 (cognition) | 🔄 scaffold + /respond mirroring; hardening pending |
| Semantic thread manager | T3 (cognition) | 🔄 clustering live; merge/split/archive pending |
| Message transformer | T3 + T4 | 🔄 all 15 ops routed; quality pass pending |
| Memory graph | T3 (phased) | 🔄 anchors + query live; full graph pending |
| Inline utility toolkit | T3 + T4 | 🔄 cognition routes live; UI wiring pending |
| Adaptive response generator | T3 → T4 | ✅ /respond landed; composer wiring pending |

---

## 3. TRACKS — DEFINITION OF DONE

### T0 Foundation ✅
- Spec + plan + branch + fork pushed.

### T1 SPA/console reskin ✅ (dev sandbox state)
- §8 tokens + chassis components + clients. Verified typecheck/build.

### T2 Message object schema ✅ (core)
- Migrations (sqlite 0041, postgres 0042), PATCH metadata route, store
  impls, analyze-on-ingest. Verified build/tests.
- **Finish items (T2-F1..F4):** Neon parity run, E2E PATCH round-trip, SDK
  regen, .svelte-kit gitignore.

### T3 Cognition service 🔄 (scaffold + Phase B core live)
- Routes live: /healthz /analyze /respond /transform /threads/cluster
  /memory/anchors|query|list. Local embeddings live. Telemetry live.
- **Phase B:** Copilot diff review → intent hardening → persona tuning →
  transform quality → thread management → /respond composer wiring.

### T4 Integration ⏳
- SPA→cognition wiring (VITE_COGNITION_URL), analyze-on-ingest already
  server-side, hover utilities live, thread sidebar + memory links resolve.

### T5 Telemetry & inspection ⏳
- Split-blade inspection (logprobs n/a on DeepSeek — show vector score,
  latency, tokens, citations), live telemetry canvas.

---

## 4. SHIP SLICES

- **Slice 1 (SHIPPING NOW):** T1 reskin + T2 schema + T3 cognition core —
  current committed state deployed and verified live.
- **Slice 2:** Standalone app identity (own URL/deploy, chat embedded) +
  T2 finish items.
- **Slice 3:** T4 integration — markers populated from real cognition data,
  hover utilities live.
- **Slice 4:** T5 telemetry + inspection depth.

---

## 5. VERIFICATION PROTOCOL (non-negotiable)

- `cd apps/web && npm run build && npm run typecheck` (or apps/logos for the
  companion) — must pass.
- `cd apps/api && go build ./... && go test ./...` — must pass.
- Migrations additive; API backward compatible.
- No `git add -A`; stage only changed files.
- Commit format: `logos: <track> — <what>`.
- Live checks after deploy: SPA 200 + served CSS hash == local build;
  /cognition/healthz ok; /cognition/analyze returns real output.

## 6. DEPLOY TOPOLOGY (current)

- Droplet `137.184.144.196`: clickclack :8090 (SQLite), logos-cognition
  :8787 (auth-gated, Cloudflare-only), logos-app :8788 (static).
- Cloudflare worker `app.catabolicsolutions.com`: /api/* → :8090,
  /cognition/* → :8787, /logos/* → :8788.
- Rebuild: `pnpm build` embeds web into Go binary; `apps/logos && npm run
  build` → /opt/logos-app/dist; `apps/cognition && npm run build` →
  /opt/logos-cognition.
- **Final target (Slice 2):** standalone URL (e.g. logos.catabolicsolutions.com),
  NOT a subpath.
