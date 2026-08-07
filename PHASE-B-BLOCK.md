# PROJECT LOGOS — Phase B/C Build Block

**Prepared by:** RINCON — 2026-08-07 08:35 MDT
**For:** GitHub Copilot CLI / VS Code CLI
**Repo:** `CatabolicSolutions/clickclack`, branch `cognitive-os`
**Auth:** GitHub Copilot CLI (device flow) or VS Code CLI — no codex, ever.
**Cost:** default deepseek-v4-pro subagents; frontier only for design review.

---

## Verified deployed state (do not regress)

- **ClickClack transport live:** `/api/*` → droplet :8090 (SQLite, messages table
  rebuilt with NOT NULL DEFAULT metadata columns: `intent`/`persona`/`confidence`/
  `context_json`/`metadata_json`/`transform_history_json`; `PATCH
  /api/messages/{id}` metadata-only updates live)
- **Cognition service live** on droplet :8787 (systemd `logos-cognition`):
  `/healthz` `/analyze` `/transform` `/threads/cluster`
  `/memory/anchors|query|list` — local embeddings (transformers.js
  all-MiniLM-L6-v2), real DeepSeek analyze/transform, telemetry
  (`latency_ms`, `total_tokens`, `model`, `execution_stack`,
  `intent_vector_score`, `memory_citations`), clarification prompts,
  cluster threshold 0.25
- **LOGOS SPA live** at `https://app.catabolicsolutions.com/logos` (droplet
  :8788 static, `logos-app.service`; CF worker proxies `/logos/*` and
  `/cognition/*`, injects cognition bearer token server-side)
- **Cloudflare worker routes:** `/api/*`, `/cognition/*`, `/logos/*` → droplet;
  everything else → container (current ClickClack UI, new metadata chips)

## Key source

- `apps/logos` (SvelteKit companion: chassis A+B, `CognitiveMarkers`,
  `ChatStream`, `SemanticThreadPane`, `InspectorBlade`, `CommandPalette`,
  `clickclack/{api,chat,types}.ts`, `cognition.ts`, `telemetry.ts`, `ui.ts`)
- `apps/cognition` (Hono TS service, `LlmClient` interface, `JsonFileStore`)
- Reference: `apps/web/src/lib/realtime.svelte.ts` (cookie WS + tail
  bootstrap), `packages/sdk-ts` (bearer variant)

## Next work (highest value first)

1. **PHASE C — wire SPA→cognition on ingest:** when a message arrives
   (poll or WS `message.created`), POST `/cognition/analyze`, PATCH the
   message metadata via `/api/messages/{id}`, populate markers + intent
   band + confidence bar from real data. Hover utilities
   (Transform/Summarize/Expand/Rewrite) → `/cognition/transform` →
   PATCH `transform_history_json`. Thread sidebar → `/threads/cluster`;
   memory links → `/memory/query`.
2. **PHASE B — intelligence:** real intent parser (six buckets), persona
   engine (five personas), `/respond` adaptive companion replies with
   mirroring + proactive clarification; wire into `ChatStream`.
3. **Fix known gaps:**
   - `apps/logos/src/lib/telemetry.ts` hardcodes `pipeline:"poll"` — read
     `$chat.realtime` instead
   - verify worker WebSocket Upgrade for `/api/realtime/ws` end-to-end
     (live browser session test)
   - LOGOS boot auto-selects FIRST workspace/channel — add explicit
     workspace/channel picker or configurable default so Conor lands
     on the right channel
4. **Standalone direction (LOGOS_HANDOFF §3):** app is a companion with
   own URL (`logos.catabolicsolutions.com` option via same CF account),
   chat is embedded component only — decide framework/deploy/auth with
   Conor before large refactor.

## Verify before shipping

- `cd apps/cognition && npm run build` && tsc clean; smoke `/healthz`
- `cd apps/logos && npm run build` (vite) + typecheck
- `pnpm build` at repo root (web embed) + `go build ./apps/api/...`
- `go test ./apps/api/...` green
- Full round-trip: create message → `/cognition/analyze` → PATCH metadata
  → UI shows intent/persona/confidence → hover transform → history pull
  shows `transform_history`

## Deploy (when green)

- **Cognition:** ship `dist/index.js` → droplet `/opt/logos-cognition/` →
  `systemctl restart logos-cognition`
- **LOGOS:** ship `apps/logos/dist` → droplet `/opt/logos-app/dist`
- **API/web:** `wrangler deploy` (CF creds from
  `CREDENTIALS.MD/clickclack_cloudflare_env.txt`) + droplet binary swap
  with backup

## Known issue

OpenAI embeddings key invalid — all-MiniLM local embeddings already
replace it; keep that path.

Commit to `cognitive-os`, push fork. Report back with what landed +
verification evidence.
