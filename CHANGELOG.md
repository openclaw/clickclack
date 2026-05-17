# Changelog

## Unreleased

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
