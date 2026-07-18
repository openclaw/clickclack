# Message reactions behavior proof

## Real ClickClack run

![A real ClickClack channel showing a sent message with a thumbs-up reaction](https://raw.githubusercontent.com/openclaw/clickclack/main/docs/proof/message-reactions-real-app.png)

The screenshot was captured by `tests/e2e/message-reactions.spec.ts` against a
locally built ClickClack production bundle served by the Go API. The test creates
a real workspace and channel through the ClickClack API, sends a message through
the ClickClack composer, and adds the thumbs-up reaction through the rendered
reaction picker.

The same run then reloads the page and asserts that the reaction is still
present, proving it was persisted rather than only painted optimistically.
It removes the reaction through the authenticated API and asserts that
the open ClickClack UI removes the reaction without a reload, exercising the
realtime event path. The remaining focused scenarios cover rollback isolation,
persistence of an in-flight optimistic intent across concurrent authoritative updates,
paged-history preservation, out-of-order reaction events, concurrent message
edits, navigation while a reaction refresh is in flight, and a reaction event
arriving while the active message page is still loading.
It also proves that an ambiguous failed removal reconciles with the committed
server state instead of resurrecting a ghost reaction, and that a failed
optimistic add rolls back from the latest realtime snapshot even when its
recovery request fails too. Finally, it holds an older successful add response,
applies a newer authenticated removal, and confirms the late response cannot
resurrect the reaction. A forced add conflict also reconciles the already-
committed server reaction before clearing the optimistic intent.
Reaction controls are disabled while a write is pending, preventing concurrent
emoji mutations from racing their authoritative refreshes.
It also proves a committed mutation remains visible, without a false error,
when an unrelated realtime update lands first and the follow-up refresh fails.

## Reproduction

```sh
REACTION_PROOF_PATH="$PWD/docs/proof/message-reactions-real-app.png" \
  pnpm exec playwright test tests/e2e/message-reactions.spec.ts
```

Expected result: `10 passed (10.9s)`.
