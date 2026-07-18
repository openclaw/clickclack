---
title: Desktop apps
description: Native ClickClack shells for macOS, Windows, and Linux, including packaging, security boundaries, and desktop-only behavior.
---

# Desktop apps

ClickClack ships one Electron desktop client for macOS, Windows, and Linux. It
connects to the hosted service by default and can point at any compatible
self-hosted server. The server remains the source of truth; the desktop client
adds operating-system behavior around the existing web app and API.

## What becomes native

- **Notifications with routing.** Incoming channel and DM notifications use the
  operating system notification center. Clicking one opens its ClickClack
  conversation.
- **Unread state outside the window.** macOS/Linux badges, a Windows taskbar
  overlay, and the tray menu reflect aggregate channel and DM unread counts.
- **Background presence.** Closing the window can keep the realtime connection
  alive in the tray so notifications still arrive. This behavior is configurable.
- **Quick compose.** `Cmd/Ctrl+Shift+K` raises ClickClack and focuses the active
  channel, DM, or thread composer.
- **Deep links.** `clickclack://app/<workspace>/<target>` opens routed workspace,
  channel, DM, and thread URLs in the desktop client.
- **Native downloads and text editing.** Completed downloads reveal themselves
  in the OS file manager. Text fields gain the platform spellchecker and native
  edit menu.
- **Remembered workspace.** Window bounds, maximized state, selected server,
  tray preference, and optional login launch are stored in the platform user-data
  directory.
- **Integrated window chrome.** ClickClack extends into the native title bar:
  macOS traffic lights and Windows/Linux caption controls share one compact row
  with the sidebar toggle, the workspace name (click it for workspace
  settings), the current channel or DM title, and centered message search, all
  on one continuous chrome surface with the rail and sidebar — the conversation
  floats on it as a rounded card with no header row of its own. Desktop
  settings live in the app menu (Cmd/Ctrl+,) and the tray menu, and a
  "Connecting…" note appears beside the channel title only while the realtime
  link is down. The browser app keeps these controls in the normal app header. The
  desktop app checks for this renderer capability before hiding the standard
  frame, so older self-hosted servers retain usable native window chrome. While
  the integrated frame is active, non-app pages open in the system browser so
  the desktop window always keeps its draggable control row.

The desktop shell does not run ClickClack server code, read agent transcripts,
or grant web content filesystem or Node.js access.

## Connect a server

Open **ClickClack → Settings** on macOS or **File → Settings** on Windows/Linux.
Enter the server origin:

```text
https://app.clickclack.chat
https://chat.example.com
http://127.0.0.1:8080
```

Remote servers must use HTTPS. Plain HTTP is accepted only for `localhost`,
`127.0.0.1`, and `::1`. Authentication returns to Electron's persistent browser
session and remains scoped to the selected origin.

GitHub sign-in opens in the system browser, where existing GitHub sessions,
passkeys, password managers, and two-factor authentication already work. After
GitHub approves the login, `chat.clickclack.desktop:/auth/callback` returns a
one-time grant to the running app. The app redeems it against the exact server
that initiated the flow, verifies the resulting session through `/api/me`, and
then reloads itself as the signed-in workspace. The app also accepts the legacy
`clickclack://auth/callback` format when connecting to an older server.

Servers using namespaced cookies require desktop OAuth protocol 2. They return
an update-required page before sending an older desktop client to GitHub.

## Security model

The app loads the selected ClickClack origin with Electron sandboxing,
`contextIsolation`, `webSecurity`, and Node integration disabled. The preload
bridge exposes only bounded notification, unread-count, navigation, and quick-
compose messages. It does not expose arbitrary IPC, shell commands, environment
variables, filesystem access, or credentials.

Navigation stays on the configured ClickClack origin. GitHub OAuth and other
HTTP(S) and mail links open in the system browser. The callback carries only an
opaque, short-lived grant: GitHub access tokens and ClickClack session tokens
never appear in the callback URL. Redemption requires the verifier held by the
initiating app, is single-use, survives server restart or replica handoff, and
expires after five minutes. Permission requests from remote content are denied.
Server configuration is accepted only from the bundled local settings window
and is written atomically with user-only permissions.

## Build locally

Install workspace dependencies, then build or run the desktop package:

```sh
pnpm install
pnpm build:desktop
pnpm dev:desktop
```

Create an unpacked app for the current platform:

```sh
pnpm --filter @clickclack/desktop run pack
```

Create installers on their native CI runner:

```sh
pnpm --filter @clickclack/desktop run dist:mac
pnpm --filter @clickclack/desktop run dist:win
pnpm --filter @clickclack/desktop run dist:linux
```

Pull requests run a three-platform desktop workflow and attach explicitly
unsigned preview installers for seven days. Official macOS release candidates
are built on an authorized maintainer Mac from the exact signed tag, signed
inside-out with the OpenClaw Foundation Developer ID identity and hardened
runtime, notarized, stapled, and uploaded to a private draft. The release
workflow independently verifies their checksums, bundle seals, stable bundle
identifier, Foundation team, Gatekeeper assessment, and notarization tickets
before publishing them alongside the Windows and Linux installers.

## Icon system

`apps/desktop/assets/icon-source.svg` is the source of truth: opposing claws for
conversation, a central aqua realtime pulse, and ClickClack coral. Generated
assets include multi-resolution macOS `.icns`, Windows `.ico`, Linux PNG, a
monochrome macOS tray template, and a Windows unread overlay.
