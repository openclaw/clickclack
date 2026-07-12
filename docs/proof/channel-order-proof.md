# Draggable channel ordering proof

Original recording commit: `3b2adc32bdc1e4fc1c2db1e1febd19fd06cea3db`

## Final integration

The recording captures the contributor's original drag, reload-persistence,
and keyboard behavior. The final current-main integration additionally keeps
ordering controls out of collapsed channel sections, preserves only the active
and unread channels while collapsed, and provides tap-accessible Move up and
Move down actions.

The final behavior is covered by
`tests/e2e/sidebar-channel-order.spec.ts`, including workspace isolation,
invalid saved state, and unavailable browser storage. The original recording
is retained as visual evidence rather than presented as a recording of those
later integration fixes.

## Video

[Watch the 7-second behavior proof](https://github.com/jjjhenriksen/clickclack/blob/codex/channel-sidebar-order/docs/proof/channel-order-drag.mp4?raw=1).

The recording shows:

1. The API-provided alphabetical order: `aa`, `mm`, `zz`.
2. `zz-video-proof` dragged above `aa-video-proof`.
3. The non-alphabetical order surviving a full page reload.
4. The focused drag handle moving the channel with the Arrow Down key.

The video is a Playwright recording of the production build at 1280 by 720,
encoded as H.264 MP4. The recorded test passed in 7.5 seconds.

## API boundary

The machine-readable capture is in
[`channel-order-api-proof.json`](https://github.com/jjjhenriksen/clickclack/blob/codex/channel-sidebar-order/docs/proof/channel-order-api-proof.json).

`GET /api/workspaces/{workspace_id}/channels` continued to return `aa`, `mm`,
`zz` after the UI had persisted `zz`, `aa`, `mm`. A `PATCH` containing only
`{"position":1}` returned the unchanged channel without a position field. This
proves that the PR does not add or imply server-side ordering.

## OpenClaw round trip

The local OpenClaw installation was `2026.7.2 (57af2bb)`. Human-authored
request `msg_01kx9r0d1yxvb8ywf9qmrtmn5z` received the exact correlated body
`fakeco-canary-ok channel-order-3b2adc3` from Dragon Lady, Nighthawk, and
Mustang in thread replies.

The canary polling process timed out despite those durable replies, so the
evidence records the subsequent thread read rather than claiming a successful
canary exit code.
