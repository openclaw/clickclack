# ClickClack Companion App — PWA Plan

**Goal:** Turn the ClickClack web SPA into an installable companion app for web + mobile
(PWA first; Capacitor later if we want app-store presence + native push).

**Repo:** `github.com/CatabolicSolutions/clickclack` (fork of `openclaw/clickclack`)
**Working tree to edit:** `apps/web` (SvelteKit SPA)

## Verified facts (2026-08-05)

- `apps/web` = SvelteKit + `@sveltejs/adapter-static`, builds to `apps/web/dist`
  (`pages: "dist"`, `assets: "dist"`, `fallback: "200.html"`, `strict: false`)
- The built SPA is **embedded into the Go binary**:
  `apps/api/internal/webassets/webassets.go` → `//go:embed all:dist`
- No `manifest.webmanifest`, no service worker, no apple-touch-icon today
  (`apps/web/static/` contains only `favicon.svg`)
- Live server: `https://137.184.144.196:8443` → nginx → `127.0.0.1:8090`
  (WebSocket upgrade headers already configured)
- Server already has push plumbing: `PushoverNotifier` / `PushNotification` in
  `apps/api/internal/httpapi` — web push can reuse or extend this
- Data: SQLite at `/var/lib/clickclack/clickclack.db`; uploads + logs alongside

## Hard constraint — secure context

Service workers and PWA install **require a trusted HTTPS origin**. The current
`:8443` listener uses a **self-signed cert** — browsers will NOT treat it as a
secure context, so SW registration and install prompts will fail there.

**Options (pick one before/while building):**
1. **Cloudflare Tunnel** (recommended — domain `catabolicsolutions.com` is already
   on Cloudflare; gives a trusted `https://clickclack.catabolicsolutions.com`)
2. **Let's Encrypt** cert for the droplet IP/domain, replace `/etc/ssl/clickclack.*`
3. Local dev only: `localhost` is a secure context, so `npm run dev` is fine for
   testing SW locally — but the deployed instance needs a trusted cert.

## PWA checklist (implementation order)

### 1. App manifest
- [ ] `apps/web/static/manifest.webmanifest` (or SvelteKit-managed):
  - `name`, `short_name`, `start_url: "/"`, `display: "standalone"`,
    `background_color`, `theme_color`
  - `icons`: 192x192 + 512x512 PNG (purpose `any` + `maskable`)
- [ ] Link `<link rel="manifest">` in `apps/web/src/app.html`
- [ ] `theme-color` meta tag in `app.html`

### 2. Icons & iOS metadata
- [ ] Generate 192/512 PNG icons from `favicon.svg` (or add new icon source)
- [ ] `apple-touch-icon` (180x180) link in `app.html`
- [ ] iOS meta: `apple-mobile-web-app-capable`, `apple-mobile-web-app-status-bar-style`

### 3. Service worker
- [ ] Add SW (e.g. `vite-plugin-pwa` or hand-rolled in `apps/web/src`):
  - Precaches the app shell (`dist` assets) on install
  - Runtime cache for API responses (network-first, cache fallback)
  - Offline fallback → `200.html` (adapter-static fallback)
- [ ] Register SW from the client (guarded: only in production / https)
- [ ] Version/cache-bust on deploy (SvelteKit `version.name` already exists —
      `CLICKCLACK_WEB_VERSION` — can key the SW cache name off it)
- [ ] Verify `npx vite build` output includes SW + hashed assets

### 4. Web Push (companion notifications)
- [ ] VAPID keypair (generate; store server-side)
- [ ] Client: request permission, subscribe via `PushManager`, send subscription to API
- [ ] Server: endpoint to store subscriptions (SQLite), send payloads on
      events/messages — extend existing `PushoverNotifier`/`PushNotification` code
- [ ] SW `push` + `notificationclick` handlers
- [ ] iOS note: web push on iOS needs the app added to home screen + iOS 16.4+;
      Android works broadly. (Pushover lane remains the reliable backbone.)

### 5. Installability / UX
- [ ] Confirm install prompt fires (Chrome/Edge) and iOS "Add to Home Screen" works
- [ ] Splash/loading state for standalone launch
- [ ] Safe-area handling for notched phones (viewport-fit=cover already present)

### 6. Validation
- [ ] `cd apps/web && npm run build` passes; `npm run typecheck` passes
- [ ] Lighthouse PWA audit ≥ 90 (installable + offline + SW)
- [ ] Test on: Android Chrome (install + push), iOS Safari (home screen), desktop
- [ ] **Do not `git add -A`** — stage only the files you changed (manifest, SW,
      icons, app.html, web config, server push code, tests)

## Deploy (after implementation)

1. `cd apps/web && npm run build` → produces `dist/`
2. Build the Go binary: `cd apps/api && go build ./cmd/clickclack`
   (web assets embed automatically via `go:embed all:dist`)
3. Ship: scp binary → droplet `/opt/clickclack/clickclack` (backup existing first)
4. Restart: `sudo systemctl restart clickclack.service`
5. Verify: `curl https://137.184.144.196:8443/` + SW/manifest reachable

## Out of scope (later)
- Capacitor wrapper → real iOS/Android apps, store distribution, native push
- Desktop companion (repo already has `apps/desktop` with signed macOS builds —
  good reference for the mobile wrapper pattern)
