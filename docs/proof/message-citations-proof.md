# Message citations behavior proof

This proof runs the built production web bundle against the real Go API and a
fresh SQLite store, then exercises the rendered browser UI.

## Reproduction

```sh
MESSAGE_CITATION_HIGHLIGHT_PROOF_PATH=docs/proof/message-citation-highlight.png \
MESSAGE_CITATION_FALLBACK_PROOF_PATH=docs/proof/message-citation-fallback.png \
pnpm exec playwright test tests/e2e/message-citations.spec.ts --workers=1
```

The scenario verifies:

- a new channel root receives an immutable public `M...` route immediately;
- desktop and touch actions copy the same absolute canonical URL;
- opening that URL before any replies keeps the channel timeline open and
  highlights the cited root;
- the URL remains byte-for-byte identical after the first reply and then opens
  the thread pane;
- replies do not receive independent citation URLs; and
- denied clipboard access exposes the full URL in a focused, selected,
  read-only fallback.

`message-citation-highlight.png` captures the direct citation before the first
reply. `message-citation-fallback.png` captures the same URL after the first
reply with the clipboard fallback open.
