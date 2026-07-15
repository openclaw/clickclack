---
read_when:
  - changing search, FTS5 triggers, Postgres text indexes, or search pagination
---

# Search

`GET /api/search` searches messages visible to the authenticated actor. It
requires `workspace_id` and `q`.

Optional parameters:

- `channel_id`: search one channel.
- `direct_conversation_id`: search one direct conversation. The actor must be a
  conversation member.
- `sort`: `relevance` (default) or `newest`.
- `limit`: page size, default 50 and capped at 100.
- `cursor`: opaque `next_cursor` from the previous page.

`channel_id` and `direct_conversation_id` are mutually exclusive. Without
either parameter, search covers visible channel messages in the workspace and
does not include direct messages.

The response is `{ "results": [...], "next_cursor": "..." | null }`. Results
contain routing metadata, author identity, thread summary fields, a bounded
plain-text snippet, and Unicode code-point highlight ranges. They do not embed
the full message body or expose backend rank values. Clients should render the
snippet as text and emphasize each `[start, end)` range.

Cursors are bound to the authenticated user, workspace, scope, query, and sort.
A cursor cannot be reused for a different search. Search uses stable
rank/time/id ordering and fetches one extra row to determine whether another
page exists; it does not run a total-count query.

## Query behavior

Whitespace-separated terms are matched together. Search operators and quoting
characters are treated as text so SQLite and Postgres expose the same safe
query behavior. Empty `q` returns an empty page without running full-text
search. Queries longer than 500 Unicode code points are rejected.

Deleted messages and agent activity rows are excluded. Guest searches remain
limited to the guest channel.

## Indexing

SQLite uses the `messages_fts` FTS5 table with the `porter unicode61` tokenizer.
Workspace and body terms are intersected inside FTS so one workspace does not
scan matches from another. Triggers keep ordinary message rows synchronized
when bodies are inserted, updated, or deleted.

Postgres uses `to_tsvector('simple', body)` with partial GIN indexes for channel
messages and direct messages. A separate direct-conversation scope index keeps
DM filtering bounded.
