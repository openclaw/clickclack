# PROJECT LOGOS — Hyper-Utility Cognitive Messaging System

**Status:** ACTIVE BUILD (kickoff 2026-08-06)
**Owner:** Conor Ross / RINCON
**Repo:** `CatabolicSolutions/clickclack` (fork of `openclaw/clickclack`)

## Vision

Re-architect the core communication model from linear text threads into an
object-oriented, cognitive substrate: an intelligent partner, semantic router,
and native operational toolset. Every message is a data object with intent,
persona, context, confidence, thread affiliation, and inline utility.

## Architecture (locked)

```
┌─────────────────────────────┐       ┌──────────────────────────────┐
│  THE GLASS (substrate)      │       │  THE BRAIN (cognition svc)   │
│  clickclack SPA + Go API    │◄─────►│  intent parser / persona      │
│  Svelte 5 + SQLite + WS     │ HTTP  │  engine / transformers /      │
│  renders message objects    │       │  semantic threads / memory    │
│  with state markers         │       │  graph (LLM + embeddings)     │
└─────────────────────────────┘       └──────────────────────────────┘
```

- **Substrate (the glass):** existing clickclack — Go API, SQLite, Svelte 5
  SPA (`apps/web`), realtime WS. API-first; UI-independent.
- **Cognition service (the brain):** separate service (planned: `apps/cognition`,
  TypeScript/Hono). Owns ALL intelligence: intent classification, persona
  rendering, inline transforms, semantic thread clustering, memory anchors.
  ClickClack stays fast and dumb; the brain is swappable.
- **Contract:** the SPA renders whatever metadata the API returns. Markers
  render as absent states until the cognition service is live.

## Message Object Schema (every message)

```json
{
  "content": "String",
  "intent": "String",
  "persona": "String",
  "context": "Object / String",
  "thread_id": "String",
  "confidence": "Float",
  "metadata": "Object",
  "transform_history": "Array"
}
```

Persisted on the message row: `intent`, `persona`, `confidence`,
`context_json`, `metadata_json`, `transform_history_json` (additive migration).

## Intents (classification buckets)

`ask` · `command` · `reflect` · `draft` · `clarify` · `explore`

## Personas (adaptive rendering filters)

`operator` (terse/tactical) · `analyst` (structured/logical) · `creative`
(generative/metaphorical) · `socratic` (probing/challenging) · `archivist`
(memory-grounded/historical)

## Inline Transform Ops (non-destructive, per-message)

`summarize` · `expand` · `rewrite` · `counterargument` · `alternative_framing`
· `diagram` · `checklist` · `plan` · `persona_rewrite` · `condense` · `extract`
· `invert` · `simulate` · `draft` · `diagnose`

## UI / Environment Vision (typewriter-minimal)

- Strict black + off-white monochrome; **0px border-radius**; crisp grid.
- **Prohibited:** speech bubbles, emojis, gradients, decorative shadows,
  consumer embellishments.
- High-density typography (Geist / Geist Mono), precision alignment.
- Message state markers: intent color band (edge), persona tag, confidence
  indicator (density bar / numeric), thread affiliation marker, execution
  status marker.
- Non-modal inline hover/tap utility bar per message (no pop-ups/modals):
  Transform, Summarize, Expand, Thread Link, Memory Link, Persona Switch.
- Sub-surface inspection (later): hover/click reveals split-blade panel with
  vector distances, confidence weightings, token-level probabilities, model
  generation params.
- Live telemetry canvas (later): execution stacks, memory-graph citations,
  intent parser metrics, active state variables.

## Tracks & Milestones

### T0 — Foundation (DONE at kickoff)
- [x] Architecture decision (brain = separate service)
- [x] This plan doc + feature branch `cognitive-os`
- [ ] Message Object schema locked in OpenAPI + Go types

### T1 — SPA reskin (typewriter-minimal operator UI)
- [ ] Design token system in `apps/web/src/styles`
- [ ] Message rendering: flat rows (no bubbles), intent band, persona tag,
      confidence, thread marker, execution marker (graceful absent states)
- [ ] Non-modal inline utility bar (handler stub, configurable cognition URL)
- [ ] Thread pane + sidebar + composer restyle; strip crustacean/playful theme
- [ ] Build + typecheck pass

### T2 — Message object schema in API
- [ ] Additive migration (next after 0040): intent/persona/confidence/context/
      metadata/transform_history columns
- [ ] sqlc queries + Go message struct updates (omitempty)
- [ ] Additive API: PATCH /messages/{id}/metadata + fields in GET responses
- [ ] OpenAPI updated; backward compatible; build + tests pass

### T3 — Cognition service (`apps/cognition`, separate process)
- [ ] Service scaffold (Hono/TS, health endpoint)
- [ ] POST /analyze → {intent, persona, confidence, context}
- [ ] POST /transform {content, op, persona?} → transformed content + meta
- [ ] Semantic threads: embeddings + clustering, cross-thread retrieval
- [ ] Memory anchors: pin message → memory node; /memory/query
- [ ] LLM provider wiring (DeepSeek/OpenAI key from env)

### T4 — Integration
- [ ] SPA → cognition service wiring (VITE_COGNITION_URL)
- [ ] Analyze-on-ingest pipeline (or on-demand) → PATCH metadata
- [ ] Hover utilities live; markers populated
- [ ] Thread sidebar shows semantic threads; memory links resolve

### T5 — Telemetry & inspection (later milestone)
- [ ] Split-blade inspection panels (vector distance, confidence, logprobs)
- [ ] Live telemetry canvas (execution stacks, parser metrics, citations)

## Verification Protocol (non-negotiable)

- `cd apps/web && npm run build && npm run typecheck` must pass
- `cd apps/api && go build ./... && go test ./...` must pass
- Migration must be additive; existing API contract stays backward compatible
- **No `git add -A`** — stage only files you changed
- Commit messages: `logos: <track> — <what>`

## Deploy (existing path, unchanged)

1. `cd apps/web && npm run build`
2. `cd apps/api && go build ./cmd/clickclack` (go:embed dist)
3. scp binary → droplet `/opt/clickclack/clickclack` (backup first)
4. `sudo systemctl restart clickclack.service`
5. Cloudflare worker proxies `app.catabolicsolutions.com` → droplet :8090
