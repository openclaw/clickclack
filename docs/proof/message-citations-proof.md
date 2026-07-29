# Message citations behavior proof

This proof runs the built production web bundle against the real Go API and a
fresh SQLite store, then exercises the rendered browser UI.

## Reproduction

```sh
MESSAGE_CITATION_HIGHLIGHT_PROOF_PATH=docs/proof/message-citation-highlight.png \
MESSAGE_CITATION_FALLBACK_PROOF_PATH=docs/proof/message-citation-fallback.png \
MESSAGE_CITATION_COPY_FRAME_PATH=/tmp/message-citation-01-copy.png \
MESSAGE_CITATION_HIGHLIGHT_FRAME_PATH=/tmp/message-citation-02-highlight.png \
MESSAGE_CITATION_THREAD_FRAME_PATH=/tmp/message-citation-03-thread.png \
MESSAGE_CITATION_FALLBACK_FRAME_PATH=/tmp/message-citation-04-fallback.png \
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

The four optional frame paths add a clearly marked proof annotation containing
the current canonical URL. The committed
`message-citation-lifecycle.png` storyboard combines those real built-app frames
in order:

1. the channel-root action menu exposes **Copy link**;
2. the copied route opens and highlights the root before replies;
3. the same route opens the thread after the first reply; and
4. clipboard denial exposes that same URL selected in the accessible fallback.

The annotation is proof-only DOM added immediately before each screenshot and
removed afterward; it does not alter the application bundle or behavior.

## Channel and direct-message persistence boundary

Channel roots and direct-message roots have separate creation paths:

- `CreateMessage` accepts a `ChannelID`, inserts through
  `InsertChannelMessage`, and eagerly assigns the channel root an `M...` route;
- `CreateDirectMessage` accepts a `ConversationID`, inserts through
  `InsertDirectMessage`, and does not assign an `M...` route; and
- `EnsureThreadRouteID` preserves the existing compatible lazy route for a
  direct-message root when that thread path is actually used.

The same boundary is covered against both stores:

```sh
go test ./apps/api/internal/store/sqlite -run TestRouteIDsCreationResolutionAndPermissions
CLICKCLACK_POSTGRES_TEST_DSN="$POSTGRES_DSN" \
  go test ./apps/api/internal/store/postgres \
  -run TestMessageRouteIDCreationRespectsChannelAndDirectBoundaries
```

## Existing-store upgrade coverage

SQLite and PostgreSQL have different route-ID schema histories. SQLite added
the columns in `0011_public_route_ids.sql`; PostgreSQL shipped them in
`0001_schema.sql`. The one-time citation backfill keys off the appropriate
marker for each backend.

The upgrade tests start from those real historical schemas, insert legacy rows,
run the current migrator, and verify that workspace, channel, direct
conversation, and channel-root routes are assigned while direct-message roots
and replies remain uncited. They also verify that the one-time completion marker
is recorded.

```sh
go test ./apps/api/internal/store/sqlite \
  -run 'TestMigrateBackfillsRouteIDsOnce|TestRouteIDBackfillAllChannelRoots'

CLICKCLACK_POSTGRES_TEST_DSN="$POSTGRES_DSN" \
  go test ./apps/api/internal/store/postgres \
  -run 'TestMigrateBackfillsLegacyChannelRootRouteIDs|TestMessageRouteIDCreationRespectsChannelAndDirectBoundaries'
```
