---
read_when:
  - changing search, FTS5 triggers, or query parsing
---

# Search

Workspace-scoped full-text search. SQLite uses FTS5; Postgres uses native
text search.

## Endpoint

```http
GET /api/search?workspace_id=&channel_id=&q=&limit=
```

Returns bounded plain-text excerpts with Unicode code-point highlight ranges:

```jsonc
{
  "results": [
    {
      "message": Message,
      "rank": <backend score>,
      "snippet": "the matched message excerpt",
      "highlights": [{ "start": 4, "end": 11 }]
    }
  ]
}
```

Clients render `snippet` as text and may emphasize each `[start, end)` range.
Offsets count Unicode code points rather than UTF-8 bytes. The web client does
not inject search-generated HTML.

`limit` is clamped to `1..100` (default 50). Empty `q` returns an empty list
without hitting FTS. Membership is required for `workspace_id`.

Search is channel-message-only. DM rows are explicitly excluded from this
endpoint. When `channel_id` is supplied, results are limited to that channel;
without it, results span channel messages in the workspace.

## Indexing

SQLite: a virtual table `messages_fts` mirrors `messages.body` with the
`porter unicode61` tokenizer. Three triggers keep it in sync:

- After `INSERT` on `messages`: insert into `messages_fts`.
- After `DELETE`: delete from `messages_fts`.
- After `UPDATE OF body`: delete + reinsert.

Soft-deleted messages remain in the index because the row stays around with
`deleted_at` set, but both stores exclude them from search results.

Postgres: the store queries `to_tsvector('simple', body)` with
`websearch_to_tsquery('simple', q)` and orders by `ts_rank_cd`.

## Query syntax

SQLite forwards `q` to FTS5 as a `MATCH` expression. Standard FTS5 operators
work (`"exact phrase"`, `term1 OR term2`, `term*` prefix). Postgres uses
web-search syntax. Clients should still treat user input as backend-specific
search text and surface errors cleanly.

## What is intentionally missing

- Cross-workspace global search.
- DM search. It needs a separate endpoint scoped to direct conversation
  membership.
