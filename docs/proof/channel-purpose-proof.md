# Channel purpose real-app proof

This proof runs the production web bundle against the real Go API with a fresh
SQLite database and exercises both rendered channel surfaces.

## Reproduction

```sh
PURPOSE_MAIN_PROOF_PATH=docs/proof/channel-purpose-main-updated.png \
PURPOSE_EMBED_PROOF_PATH=docs/proof/channel-purpose-embed-updated.png \
PURPOSE_CLEAR_PROOF_PATH=docs/proof/channel-purpose-cleared.png \
pnpm exec playwright test tests/e2e/channel-purpose.spec.ts --workers=1
```

The scenario verifies:

- a purpose supplied during real channel creation is persisted and rendered;
- the main app and embedded channel both show the initial value;
- a real API update reaches both open views through `channel.updated`, without
  either page being reloaded;
- the updated value is captured in both rendered surfaces;
- clearing the purpose removes it from both open views in realtime; and
- the cleared main-app state is captured after both absence assertions pass.

The proof uses generated workspace/channel identifiers and local development
authentication. It contains no hosted token, private endpoint, or user data.
