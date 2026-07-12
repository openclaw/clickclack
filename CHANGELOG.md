# Changelog

## Unreleased

- Added personal channel ordering with desktop drag-and-drop, keyboard and touch controls, per-user/workspace browser persistence, and same-profile tab synchronization. Thanks @jjjhenriksen.
- Added independently collapsible Channels, Direct messages, and People sidebar sections that retain active and unread rows while collapsed, with accessible controls and per-workspace browser persistence. Thanks @jjjhenriksen.
- Unified the web app on one warm "paper & ember" design language: a new espresso dark theme replaces the cool slate look, identity marks share a theme-tied brand gradient, off-palette purple/indigo/blue hardcodes (including workspace-settings buttons and quote blocks) now follow the coral accent, settings surfaces inherit the app font, and scrollbars are visible on the light theme.
- Added manager workspace profile and icon controls plus owner-only ownership transfer and permanent deletion with durable upload cleanup. Thanks @Solvely-Colin.
- Proxied WebSocket upgrades in the Vite dev server so the local app connects to realtime instead of looping on "Reconnecting…".
- Added a bounded read-only artifact pane for code, text, Markdown, PDF, and sandboxed HTML attachments, while keeping DOCX download-only and preserving thread routes when the pane closes. Thanks @jjjhenriksen.
- Added an opt-in realtime tail cursor so fresh clients can skip retained history without racing new events, and applied read-receipt visibility before event pagination. Thanks @shakkernerd.
- Preserved validated request correlation IDs as optional metadata on durable message and thread-reply events across replay and realtime delivery, and added canary run/case evidence IDs without changing message storage or gateway traffic.
- Added an isolated FakeCo small-VM deployment path with idempotent synthetic chat seed data, OpenClaw and ClawRouter SecretRef configuration, correlated health/readiness and metadata-only telemetry, a quoted-reply end-to-end canary, tests, and teardown guidance.

## 0.1.0 - 2026-07-06

- Published automated macOS, Windows, and Linux desktop installers with verified SHA-256 manifests alongside every tagged GitHub Release.
- Integrated the desktop sidebar toggle and message search into native macOS, Windows, and Linux title bars while preserving the browser app header.
- Added secure ClickClack desktop apps for macOS, Windows, and Linux with native notifications, unread badges, tray lifecycle, quick compose, deep links, downloads, spellcheck, and self-hosted server selection.
- Opened desktop GitHub sign-in in the system browser, then returned securely through a short-lived, verifier-bound `clickclack://` callback so existing browser sessions, passkeys, and two-factor authentication work without exposing tokens in URLs.
- Served clickclack.chat and www.clickclack.chat from the Cloudflare Worker as declarative custom domains in `wrangler.jsonc`, retiring the separate Hetzner web host; container image rollouts are forced by bumping the dated `CLICKCLACK_CONTAINER_NAME`.
- Redesigned the clickclack.chat product website with a modern Geist-based look that matches the app: sticky nav, app-mockup hero, feature grid, CLI terminal, quickstart steps, and full dark-mode support.
- Refreshed runtime, build, and CI dependencies to clear known JavaScript advisories and pin current GitHub Actions releases.
- Wrote CLI client configuration atomically while preserving read-only-file and symlink safety. Thanks @KrasimirKralev.
- Added bot-scoped durable agent commentary and tool activity, collapsed into per-turn preambles with independent visibility controls and configurable self-message alignment. Thanks @ragesaq.
- Added persistent thread activity counts and row-click thread opening in channel and DM timelines, with live summary refreshes. Thanks @PollyBot13.
- Added per-user DM closing with durable history, direct-link access, Undo, canonical one-to-one reopening, and automatic resurfacing on new messages. Thanks @PollyBot13.
- Kept self-hosted landing-page App links on the instance-local `/app` route for Tailscale, LAN, and custom hostnames instead of redirecting to the hosted app. Thanks @iXandru for the report and @TurboTheTurtle for the fix.
- Reused the canonical two-member direct conversation for repeated DM creates so bot replies no longer split across duplicate chats. Thanks @PollyBot13 for the report.
- Added opt-in browser notifications for incoming channel and direct messages, including click-through navigation. Thanks @PollyBot13.
- Rendered ephemeral agent progress activity lines in web chat for live tool, thinking, plan, patch, and command updates. Thanks @ragesaq.
- Allowed normal bot-write tokens to publish ephemeral agent progress while keeping progress frames bot-only and target-scoped. Thanks @ragesaq and @shakkernerd.
- Fixed the mobile sidebar collapse control so it closes the open drawer on narrow screens without changing the desktop collapse preference. Thanks @iXandru for the report.
- Fixed the mobile web layout on iPhone Safari so the closed navigation drawer fully leaves the viewport and the composer respects safe areas.
- Revived the platform app APIs for bot lifecycle, app installation, slash
  command registration/invocation, event subscriptions, connected accounts,
  audit events, and message topics; the web composer now previews registered
  slash commands on `/` and workspace mentions on `@` without shifting the
  message timeline.
- Rejected negative byte sizes when finalizing reserved uploads so quota accounting cannot be credited by malformed store calls.
- Kept the infra SQLite migration mirror in sync with the embedded guest moderation migration.
- Required DM bot scopes on generic message, thread, reaction, upload, and typing routes when the target resource belongs to a direct conversation.
- Reserved upload quota before object storage writes so concurrent uploads cannot overspend a workspace budget.
- Stopped blocked or timed-out moderators from receiving private member-moderation events.
- Prevented workspace moderators from moderating privileged bot members.
- Reserved the `guest` channel name, locked Postgres moderation role checks during member updates, and bounded direct-conversation member sets after DeepSec review.
- Hardened JSON request body limits, realtime typing scope validation, and upload attachment authorization after DeepSec review.
- Documented guest-room moderation, upload previews, Postgres/R2 deployment notes, and sqlc store workflow.
- Added a guest waiting-room moderation flow with three-post daily guest limits, OpenClaw GitHub org moderator mapping, member approval, timeouts, blocks, and moderator profile controls.
- Added first-page thumbnail previews for PDF uploads, thumbnail cards for text uploads, inline audio controls in chat messages, and open GitHub guest-room sign-in.
- Redacted stored token hashes and upload storage paths from JSON exports, and constrained local upload serving/deletion to the configured upload directory.
- Bounded outbound upload and GitHub OAuth HTTP calls with default timeouts, streamed R2 upload saves without full in-memory buffering, and constrained R2 object access to the configured prefix.
- Allowed upload clients to pass `workspace_id` as a query parameter so multipart file parts no longer depend on form field ordering.
- Disabled public magic-link request minting when dev auth is off, keeping CLI-created magic links as the production path until delivery is configured.
- Restricted dev-bootstrap magic-link token minting to loopback clients.
- Restricted dev-bootstrap user-header and first-user fallback auth to loopback clients.
- Defaulted session cookies to `Secure` outside local dev HTTP, even when proxy HTTPS headers are missing.
- Kept Pushover notification HTTP calls bounded even when a notifier is constructed without an explicit client.
- Included channel and direct-message read receipts in JSON exports.
- Exported JSON from one read transaction snapshot for SQLite and Postgres stores.
- Redacted Pushover user keys from JSON exports.
- Stopped request logs from recording OAuth callback query secrets and clear GitHub OAuth state cookies after callback validation.
- Redacted invite tokens from JSON exports.
- Required message-write scope and message authorship before attaching uploads to messages.
- Rejected cross-site or non-JSON magic-link consume requests before setting session cookies.
- Capped JSON request bodies and added full-request read timeouts to reduce slow or oversized request abuse.
- Hid and detached uploads from deleted messages so deleted chat content is no longer exposed through attachment history or upload fetches.
- Made `/api/me` profile and notification setting updates atomic so failed validation no longer persists partial profile changes.
- Restricted channel rename, archive, and kind changes to workspace owners and scoped bots, while preserving an owner for OAuth-created default workspaces.
- Serialized Postgres migrations with an advisory lock to avoid multi-instance startup races.
- Kept the infra SQLite migration mirror in sync with the embedded auth token-hash migration.
- Made channel and direct-message read receipt writes monotonic in SQLite and Postgres so concurrent older updates cannot move read cursors backwards.
- Rejected cross-site browser requests from local dev magic-link minting and fallback auth even when the TCP peer is loopback.
- Serialized SQLite message sequence allocation so concurrent channel, direct-message, and thread writes cannot collide on the same next sequence number.
- Made repeated message deletes idempotent in SQLite and Postgres so deletion timestamps and durable events are not rewritten.
- Parsed auth expiration and event retention timestamps instead of comparing raw timestamp text in SQLite and Postgres.
- Joined GitHub allowed-org users to the explicit ClickClack default workspace instead of whichever workspace was created first.
- Added a Postgres attachment lookup index so upload visibility checks can find shared messages by upload ID efficiently.
- Limited the default-workspace owner repair migration to the ClickClack workspace so public guest workspaces are not promoted accidentally.
- Returned saved notification settings from session-authenticated `/api/me` responses so profile saves do not clear Pushover preferences.
- Made concurrent Postgres default workspace joins retry the winning workspace instead of failing first GitHub org logins.
- Published realtime message update events when uploads are attached so other clients refresh message attachments immediately.
- Returned moderation-restricted errors for blocked channel update attempts before checking channel-admin privileges.
- Rejected cookie-authenticated unsafe API requests unless they carry the first-party CSRF header and pass same-origin browser checks.
- Required ephemeral realtime events with `channel_id` to target a channel the sender can access in the requested workspace.
- Blocked legacy direct-message realtime backlog events for guests when payloads only referenced message IDs.
- Preserved existing SQLite magic links and sessions while backfilling token hashes during auth-token migrations.
- Prevented late message edits from rewriting a message body after the message has been deleted.
- Reserved system workspace slugs, made SQLite default workspace joins concurrency-safe, and rechecked channel access before replaying nonce-idempotent messages.
- Added per-user workspace upload quotas so unattached uploads cannot grow storage without bound.
- Blocked moderated users from publishing realtime typing and presence events.
- Added a bounded indexed lookup for upload attachment visibility checks.
- Hardened production defaults and deepsec-reported security edges: dev auth now requires explicit opt-in, Docker starts fail-closed, WebSockets enforce origin checks while supporting SDK bearer-token protocols, uploads no longer expose storage paths or execute as same-origin HTML, magic-link consumption is atomic, session cookies become secure on HTTPS public URLs, and CI/release supply-chain refs are pinned.
- Added optional Cloudflare R2 upload storage via `CLICKCLACK_UPLOADS=r2://bucket/prefix`, keeping local disk as the default backend.
- Added Postgres as an alternate server store via `postgres://` / `postgresql://` DB URLs, including migrations, search, CLI env defaults, and an opt-in Postgres integration smoke test.
- Fixed clawpatch-reported CLI export/listing, config precedence, auth token redaction, bot workspace isolation, upload cleanup/privacy, deleted-message search, reaction no-op events, DM revocation, thread nonce, and coverage edge cases.
- Fixed `pnpm test` so it no longer rewrites embedded web assets, and made the release asset embedding step preserve the existing embedded app if the copy fails.
- Remember the last visited channel per workspace in the web app so returning
  to a bare workspace route no longer jumps back to the first channel in the
  list. Thanks @shakkernerd.
- Set the web app favicon to the ClickClack icon. Thanks @shakkernerd.
- Added short public route IDs for workspace, channel, DM, and thread URLs while
  preserving internal IDs and canonicalizing older copied links. Thanks
  @shakkernerd.
- Added first-class bot identities with human vs bot user kind, service-bot
  and user-owned bot records, scoped `ccb_...` bot tokens, CLI bot creation,
  bot-token auth checks, OpenAPI/SDK/web type coverage, visible bot badges,
  and a bot architecture spec for the OpenClaw extension path.
- Fixed profile contact-information icons so the glyph stays centered inside
  its square tile.
- Forced Playwright E2E runs to stay headless by default across local and CI
  invocations.
- Moved channel and direct-message creation into modal flows opened from
  right-aligned sidebar section plus buttons, removing the always-visible
  inline create fields.
- Renamed the README header to `ClickClackClaw` with a paw-print mark and a
  short OpenClaw chat tagline.
- Fixed the chat shell realtime connection badge and mobile navigation drawer
  behavior, including backdrop/Escape close handling and type-to-focus blocking
  while navigation is open. Thanks @BunsDev.
- Added optional per-user Pushover notifications for new channel, thread, and
  DM messages. Server admins configure `CLICKCLACK_PUSHOVER_API_TOKEN`; users
  opt in from account settings with their own Pushover user key. Thanks
  @thewilloftheshadow.
- Polished profile pane/profile settings actions and message hover states so
  buttons share the same height, contact icons stay centered, and hovered
  messages no longer also highlight the inner text row.
- Added cursor pagination for channel and DM history. Message lists now
  support latest, before, after, and around sequence windows with page
  metadata, while the web chat keeps a bounded active message window, loads
  older and newer history as users move through the timeline, and opens search
  or quote targets with around-sequence context without breaking the
  after-sequence API contract. Thanks @shakkernerd.
- Added route-driven chat URLs for workspace, channel, DM, and thread views so
  direct links, refresh, and browser back/forward restore the selected
  conversation without leaving stale in-memory navigation state. Thanks
  @shakkernerd.
- Fixed live-message handling while reading older history so incoming messages
  mark the current window as having newer history instead of silently paging
  the timeline forward.
- Added a persistent in-timeline "New" divider and unread bar for unread
  channel and DM messages. The divider anchors to the read boundary, the bar
  jumps back to that boundary, and users can explicitly mark the view read
  when they are done. Thanks @shakkernerd.
- Added retry-safe optimistic sends, per-user unread/read receipts for
  channels and DMs, private read events, and member-scoped DM typing
  indicators. The chat UI now shows unread badges and jump-to-bottom unread
  counts, reconciles pending sends across realtime/reload races, and exposes
  the new read APIs through OpenAPI, the TypeScript SDK, and docs. Realtime
  event cursors are now monotonic so same-millisecond events replay in order.
  Thanks @shakkernerd.
- Improved media previews and long message timelines: uploads now carry
  image/video dimensions through SQLite, API responses, OpenAPI, and web
  types; the chat timeline is virtualized with scroll restoration, bottom
  pinning, and reliable quote jumps. Thanks @shakkernerd.
- Added type-to-focus on the chat composer: pressing a printable key while
  focus is outside any text field (and no modal/menu is open) now jumps the
  caret to the active composer — the thread reply textarea when a thread pane
  is open, otherwise the channel/DM composer — so the keystroke lands as the
  next character of your draft. The composer also auto-grows as the draft
  spans multiple lines, capped at half the viewport before a scrollbar appears,
  and shrinks back to a single row after sending. IME
  composition, modifier shortcuts, text fields, menus, media controls, and
  active text selections inside messages or threads are preserved untouched.
  Thanks @shakkernerd.
- Added inline quote-replies in channels, DMs, and threads. Every
  message-create endpoint now accepts an optional `quoted_message_id`; the
  server captures a 280-rune trimmed snapshot of the quoted body plus the
  quoted author at send time, and rejects cross-context quotes with
  `HTTP 400`. The chat UI exposes a hover-revealed "Quote" affordance,
  composer chip, click-to-jump quote block, and an "[original deleted]"
  fallback when the source is hard-deleted (FK is `ON DELETE SET NULL` so
  the snapshot survives). The `clickclack send` and `clickclack threads
  reply` CLI commands gain `--reply-to msg_...` flags. See
  [docs/features/replies.md](docs/features/replies.md). Thanks @shakkernerd.
- Refined the chat app shell with denser chat navigation,
  grouped message timelines, clearer empty states, responsive sidebars, and a
  send button that no longer appears active for attachment-only drafts.
- Added richer Slack-like chat affordances: animated sidebar/thread panels,
  inline image attachment cards, Markdown composer controls, and a GIF picker
  that inserts animated GIF Markdown.
- Added Slack-style user profile side panes, automatic People shortcuts in the
  sidebar, inline video playback, tighter message spacing, and an image viewer
  modal for inline images.
- Added controlled Markdown GIF playback: GIFs play briefly, pause on a still
  preview, and expose a lower-right replay button.
- Added a public product website at the web root while keeping the chat app at
  `/app` locally and on `app.clickclack.chat` when served from that host.
- Added an agent-friendly ClickClack client mode to the Go binary with
  `login`, `logout`, `whoami`, `status`, workspace/channel listing, message
  send/list, and thread open/reply commands.
- Scoped stored CLI credentials and workspace/channel defaults to the saved
  server URL, with `--user` / `CLICKCLACK_USER_ID` taking precedence over
  stored bearer tokens unless `--token` is explicitly supplied.
- Documented the `clickclack.chat` product domain, `app.clickclack.chat` app
  domain, `docs.clickclack.chat` docs domain, and recommended bearer-token auth
  flow for hosted agents.
- Added more visible GitHub links to the product website and improved the docs
  quickstart CTA contrast in dark mode.
- Split GitHub Actions into explicit Go, TypeScript, Playwright, and Docker
  gates, with `gofmt` and `oxfmt --check` enforced in CI.
- Added GoReleaser config and release workflow for Linux, macOS, Windows, and
  FreeBSD archives, plus Linux `.deb` and `.rpm` packages.
