# Message editing behavior proof

This proof exercises the locally built ClickClack production web bundle through the real Go API and rendered browser UI.

## Reproduction

```sh
MESSAGE_EDITING_PROOF_PATH=docs/proof/message-editing-real-app.png \
CLICKCLACK_E2E_PORT=18212 \
pnpm exec playwright test tests/e2e/message-editing.spec.ts
```

Result: `3 passed (7.7s)`.

The browser tests create isolated workspaces and channels, send and edit a channel message through the rendered UI, verify the editor resolves to visible theme styles, prove only one editor mounts when the thread root is visible in two surfaces, restore focus to the initiating Edit action on Escape, preserve an unsaved draft when another Edit action is activated, reload to prove persistence, wait for a thread reply to persist before editing and reloading it, prove the client submits exact drafts while matching the server's Unicode whitespace normalization, reject normalized no-op and empty keyboard-shortcut edits, and keep the active draft plus its visible error mounted when a save fails while blocking a competing editor.

It also closes the thread while its root editor owns the global edit session,
then opens the timeline editor for that message, proving an unmounted surface
cannot leave behind a stale lock that blocks future edits.
The run also verifies that closing and reopening a thread discards its unsubmitted
root draft instead of unexpectedly remounting edit mode, and that keyboard focus
reveals the timeline action bar without requiring pointer hover.
An unsaved timeline draft is carried by the parent edit session across an SPA
channel switch that unmounts and remounts the message row.

The committed screenshot `message-editing-real-app.png` shows the resulting ClickClack channel and thread, including both edited bodies and their `(edited)` indicators.
