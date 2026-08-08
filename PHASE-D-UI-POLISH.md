# PROJECT LOGOS — Phase D: UI/UX Polish Directive

**Prepared by:** RINCON — 2026-08-07 20:36 MDT
**For:** GitHub Copilot CLI / VS Code CLI
**Repo:** `CatabolicSolutions/clickclack`, branch `cognitive-os` (canonical commit **`8f65306`**)
**Auth:** Copilot CLI (device flow) or VS Code CLI — no codex, ever.
**Cost:** deepseek-v4-pro subagents default; frontier only for design review.

---

## 0. CURRENT STATE / HOW WE GOT HERE (read first — do not skip)

- `git fetch origin` first. Canonical state = commit **`8f65306`**; BOTH
  `cognitive-os` and `catabolicsolutions-logos-phase-bc` point there and are
  identical. Your local worktree may be behind and may contain pre-existing
  uncommitted churn (line-ending noise) — do NOT commit or build from that
  dirty state; hard-reset/checkout the fetched `8f65306` before starting.
- **What's live right now:** `https://app.catabolicsolutions.com/logos` serves
  the LOGOS SPA (droplet :8788, `logos-app.service`) + cognition service
  (droplet :8787, `logos-cognition.service`), routed by the Cloudflare worker
  (`/logos/*` → :8788, `/cognition/*` → :8787 with token injection, `/api/*` →
  ClickClack :8090). ClickClack transport (metadata PATCH contract) is live.
- **Recent commits (all on the fork):** `138771d` phase-bc feature set
  (ANCHOR/COPY/DRAFT, cognition respond, substrate client) → `956c664`
  **blank-page fix**: `paths.base='/logos'` in `apps/logos/svelte.config.js`
  (root-relative `base:''` made browsers fetch `/_app/...` which the worker
  doesn't route → all assets 404 → blank page; fixed + deployed + verified
  assets 200 under `/logos/_app/...`) → `8f65306` this directive.
- **Do NOT regress:** the `/logos` base fix is committed AND live. If a build
  reverts `base:''`, the app goes blank again. The metadata PATCH contract
  (PATCH /api/messages/{id}, intent/persona/confidence/transform_history) is
  live and verified persisting — no API changes in this pass.


## 1. Problem statement (from Conor)

The app works (assets load, chats populate, ANCHOR/COPY/DRAFT functional) but the
visual + UX is **too harsh and text-block**. Wanted: **smoother, sleeker, more
intuitive** — an operator-grade companion that feels refined, not like a terminal.

## 2. What stays (do not regress)

- All functional surfaces: ANCHOR / COPY / DRAFT, CommandPalette, InspectorBlade,
  ChatStream notice/context UI, SemanticThreadPane, TelemetryRail, ClarificationPrompt.
- The metadata transport contract (PATCH /api/messages/{id}, intent/persona/confidence
  chips, transform_history) — untouched by this pass.
- The dark color-scheme identity (LOGOS stays dark; it does NOT become light).
- Operator information density where it matters (telemetry, confidence, intent) —
  keep the data, change the presentation.
- `paths.base = '/logos'` in `apps/logos/svelte.config.js` — do not touch (breaks
  the deployed subpath; assets must stay under `/logos/_app/...`).

## 3. Design direction (Conor's ask translated)

| Current (harsh) | Target (sleek) |
|---|---|
| Pure black `#000000` bg, `#050505` voids | Layered dark surfaces: deep charcoal base + subtle elevation per panel (e.g. #0E0F12 → #14161A → #1A1D22) |
| `--radius: 0px` everywhere | Soft radius: 6–10px cards, 8px inputs, 12px dialogs/modals, 999px pills/chips |
| 100–150ms **linear** motion, `steps(2)` | 150–220ms cubic-bezier(0.2, 0.8, 0.2, 1) ease; hover/enter transitions on interactive elements |
| No shadows, no glow | Subtle layered shadows (soft, low-opacity) + restrained focus rings (2px, intent-color, offset) |
| Mono-heavy metadata blocks everywhere | Hierarchy: Inter for reading, JetBrains Mono ONLY for true data (telemetry, confidence, timestamps, ids). Reduce mono noise in message bodies and action rails. |
| Dense text blocks, no whitespace rhythm | Vertical rhythm: consistent 4/8px spacing scale, 16–20px line-height on bodies, clear section gaps, breathing room in cards |
| Flat everything | Hover/active states on every interactive element; pressed feedback; focus-visible rings; cursor affordances |

## 4. Concrete work items (file-scoped)

### 4.1 `apps/logos/src/styles/tokens.css` — the core of this pass
- Replace the palette block with layered dark surfaces (keep `color-scheme: dark`).
- Radius tokens: `--radius-sm: 6px; --radius: 8px; --radius-lg: 12px; --radius-xl: 16px; --radius-pill: 999px`.
- Motion tokens: easing curve + 150/200/250ms durations; keep `--motion-step` only for
  genuinely stepped indicators (status LEDs), not general UI.
- Add shadow tokens (2–3 elevation levels, very low opacity black, subtle).
- Add focus-ring token (2px, uses intent color with alpha).
- Spacing scale tokens (4/8/12/16/24/32) if not present.
- Font roles: `--font-body` Inter (keep), keep `--font-mono` for data-only; add
  `--font-ui` if helpful. Do NOT change fonts entirely.

### 4.2 `apps/logos/src/styles/chassis.css`
- Update panel/card surfaces to use new layered tokens.
- Add consistent padding rhythm (cards ≥ 12–16px), section separators with softer rules.
- Round the main shell corners where the design allows (top-level frame, panes).
- Smooth scrollbars (already tokenized — tune track/thumb colors to new palette).

### 4.3 `apps/logos/src/lib/components/*` — component-level polish
- **MessageFrame.svelte**: rounded message cards, 2px intent edge band KEPT (identity)
  but softer (maybe 3px radius on the band, less neon at rest, brighter on hover/active).
  Metadata header: smaller, muted, spaced — not a wall of mono text.
- **ChatStream.svelte**: gap rhythm between messages (8–12px), smoother scroll,
  date/system dividers styled as subtle pills, new-message entrance animation (fade+slide 8px).
- **ResultStrip.svelte**: action buttons as rounded ghost buttons with hover fill +
  icon support; COPY/DRAFT feedback (checkmark on copy, brief toast/inline confirm).
- **CommandPalette.svelte**: centered modal, rounded, backdrop blur (if cheap),
  keyboard hints styled as kbd chips, result list with hover states + icons.
- **InspectorBlade.svelte / TelemetryRail.svelte**: keep telemetry density but group
  into labeled rows with muted labels + mono values; section headers; collapsible if cheap.
- **SemanticThreadPane.svelte**: thread chips as pills (CL-XX chips rounded), memory
  viewer cards with hover reveal; softer active/selected states.
- **ClarificationPrompt.svelte**: card with accent border, [ASK]/[DISMISS] as real
  buttons with hover/press states.
- **Empty/loading states**: add skeleton or soft pulse where data is pending; empty
  workspace/channel states should be inviting, not blank.

### 4.4 Global UX passes
- Buttons/inputs/selects: consistent rounded ghost/filled styles, focus-visible rings,
  `:active` press feedback, disabled opacity.
- Chips/badges (intent, persona, confidence): pill radius, muted backgrounds with
  colored text + small dot instead of full neon fills.
- Responsive: ensure the layout degrades gracefully to phone width (Conor uses mobile).
  Check the tiled grid/chassis at ≤480px — no horizontal overflow, tap targets ≥40px.
- Toast/notice system if trivial: success on COPY/DRAFT/ANCHOR, error on failures.

## 5. Verification before shipping

- `cd apps/logos && npm run typecheck && npm run build` green.
- `git grep -n "0px" apps/logos/src/styles/` should show only intentional zero-width
  borders/outlines, NOT radius values.
- Build produces `/logos/_app/...` asset paths (verify `paths.base` unchanged in dist
  HTML: grep `"/logos/_app/immutable/entry/` in `dist/200.html`).
- Manual smoke on desktop + narrow mobile viewport: chat loads, send works, ANCHOR /
  COPY / DRAFT render, CommandPalette opens, no console errors.

## 6. Deploy (after green)

- `cd apps/logos && npm run build` → ship `dist/` to droplet `/opt/logos-app/dist`
  (backup existing to `dist.REPLACED-<ts>`), then `systemctl restart logos-app`.
- Verify public: `https://app.catabolicsolutions.com/logos` loads + entry asset 200
  + `/cognition/healthz` 200. Confirm asset URLs are `/logos/_app/...` (the subpath
  fix MUST survive — regression = blank page).

## 7. Scope guardrails

- This is a VISUAL/UX pass. No schema changes, no API changes, no worker changes.
- If a change would require touching `apps/api`, `infra/cloudflare`, or migrations —
  STOP and report instead.
- Commit to `cognitive-os`, push fork. Report what landed + verification evidence.
