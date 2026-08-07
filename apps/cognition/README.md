# LOGOS Cognition Service (`@clickclack/cognition`)

The **brain** of PROJECT LOGOS — a separate service from the clickclack Go API
("the glass"). Owns all intelligence: intent classification, persona rendering,
inline transforms, semantic thread clustering, and memory anchors.

**Status:** SCAFFOLD (T3) — LLM wiring is a handoff task.
All routes return deterministic stub responses.

## Quick Start

```bash
# from repo root
pnpm install          # install dependencies (includes this workspace package)
pnpm --filter @clickclack/cognition dev

# or from this directory
cd apps/cognition
pnpm install
pnpm dev
```

Server starts on `http://localhost:8787`.

## Environment Variables

| Variable          | Default | Description                                    |
| ----------------- | ------- | ---------------------------------------------- |
| `PORT`            | `8787`  | HTTP listen port                               |
| `LLM_PROVIDER`    | `stub`  | LLM backend: `deepseek`, `openai`, or `stub`   |
| `DEEPSEEK_API_KEY`| —       | Required when `LLM_PROVIDER=deepseek`          |
| `OPENAI_API_KEY`  | —       | Required when `LLM_PROVIDER=openai`            |
| `CORS_ORIGIN`     | `*`     | Allowed CORS origin (set to SPA URL in prod)   |
| `DATA_DIR`        | `./data`| Directory for JSON file store                   |

Copy `.env.example` to `.env` and fill in values.

## Route Table

| Method | Path              | Description                              | Input                                  | Output                    |
| ------ | ----------------- | ---------------------------------------- | -------------------------------------- | ------------------------- |
| GET    | `/healthz`        | Health check                             | —                                      | `{ok, service, version}`  |
| POST   | `/analyze`        | Classify intent, persona, confidence     | `{content, context?}`                  | `AnalyzeResult`           |
| POST   | `/transform`      | Apply inline transform to content        | `{content, op, persona?}`              | `TransformResult`         |
| POST   | `/threads/cluster`| Cluster messages into semantic threads   | `{message_ids[], contents[]}`          | `ClusterResult`           |
| GET    | `/memory/query`   | Search memory anchors                    | `?q=...&limit=10`                      | `{query, nodes[]}`        |
| GET    | `/memory/list`    | List all memory anchors                  | —                                      | `{nodes[]}`               |
| POST   | `/memory/anchors` | Create a memory anchor                   | `{content, source_message_id?, tags?}` | `AnchorResult` (201)      |

## Scripts

| Script       | Description                        |
| ------------ | ---------------------------------- |
| `pnpm dev`   | Start dev server with hot reload   |
| `pnpm build` | Typecheck + bundle with esbuild    |
| `pnpm start` | Run bundled production server      |
| `typecheck`  | `tsc --noEmit`                     |

## Architecture

```
apps/cognition/
  src/
    index.ts          HTTP entry (Hono), routes, validation
    types.ts          Message object schema, intent/persona/op unions,
                      request/response types
    lib/
      llm.ts          LlmClient interface + StubLlmClient
      store.ts        MemoryStore + ThreadStore interfaces, JsonFileStore
  data/               JSON file persistence (auto-created)
  .env.example        Environment variable template
  package.json
  tsconfig.json
  README.md           ← you are here
```

### Interfaces

- **`LlmClient`** — `analyze()`, `transform()`, `embed()`. Swap implementations
  via `LLM_PROVIDER` env. Current: `StubLlmClient` (logs + deterministic fake).
- **`MemoryStore`** — `saveAnchor()`, `queryAnchors()`, `listAnchors()`.
- **`ThreadStore`** — `saveThread()`, `getThread()`, `listThreads()`,
  `deleteThread()`.

Both are implemented by `JsonFileStore` (writes to `./data/`).

## T3 Handoff Tasks

The scaffold is complete. Remaining LLM wiring:

1. **DeepSeek provider** — implement `LlmClient` using DeepSeek chat API for
   `analyze()` and `transform()`, DeepSeek embeddings API for `embed()`.
   Prompt engineering: construct system prompts per persona, few-shot examples
   for intent classification.
2. **OpenAI provider** — same interface, OpenAI chat + embeddings.
3. **Vector search in store** — replace naive substring match in
   `JsonFileStore.queryAnchors()` with cosine similarity over stored embeddings.
4. **Semantic clustering** — replace the stub single-cluster logic in
   `POST /threads/cluster` with real embedding-based clustering (DBSCAN or
   agglomerative).
5. **Prompt templates** — extract persona definitions, intent classifier
   prompts, and transform templates into a `src/lib/prompts.ts` module.
6. **Rate limiting / token budgeting** — add per-request token tracking,
   configurable max tokens.
7. **Response streaming** — SSE support for long-running transforms.
