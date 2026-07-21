# Message editing behavior proof

This proof exercises the built ClickClack production web bundle through the real
Go API and rendered browser UI.

## Reproduction

```sh
MESSAGE_EDITING_EDITOR_PROOF_PATH=docs/proof/message-editing-editor.png \
MESSAGE_EDITING_PROOF_PATH=docs/proof/message-editing-real-app.png \
pnpm exec playwright test \
  tests/e2e/message-editing.spec.ts \
  tests/e2e/embed-channel.spec.ts \
  tests/e2e/embed-thread.spec.ts
```

Result: `7 passed (39.4s)`.

The browser tests cover:

- channel messages, direct messages, thread roots, and thread replies;
- authenticated channel and thread embeds;
- persisted edits, edited indicators, Markdown tables, and retained reactions;
- exact draft submission with server-compatible Unicode whitespace checks;
- empty, no-op, failed, delayed, and competing save paths;
- focus restoration, keyboard controls, and theme-resolved editor styling;
- draft retention across channel switches and virtualized row recycling;
- thread-close cancellation, realtime reconciliation, and deletion cleanup.

The final suite ran through Crabbox on Hetzner:

```text
run: run_0c54703db827
lease: cbx_09128e47bb1d
machine: ccx33
result: 7 passed (39.4s)
lease stopped: true
```

The broader `pnpm check` stages also completed the production build, Go package
tests, fake company deployment tests, desktop tests, type checking, and linting.
Its only initial failure was formatting in the newly added test file; the
corrected final suite reran `pnpm fmt:check` successfully before Playwright.

`message-editing-editor.png` captures the focused inline reply editor.
`message-editing-real-app.png` captures the saved channel and thread state,
including Markdown rendering, reactions, thread structure, and edited
indicators.
