// PROJECT LOGOS — Cognition Service: HTTP entry point
//
// Hono server exposing the cognition API.
// Routes: health, analyze, transform, thread clustering, memory anchors/query.
//
// All intelligence is stubbed. LLM wiring is a T3 handoff task.

import { Hono } from "hono";
import { cors } from "hono/cors";
import { serve } from "@hono/node-server";
import type { Context } from "hono";

import {
  INTENTS,
  PERSONAS,
  TRANSFORM_OPS,
} from "./types.js";
import type {
  AnalyzeRequest,
  AnalyzeResult,
  AnchorRequest,
  AnchorResult,
  ClusterRequest,
  ClusterResult,
  HealthResponse,
  MemoryQueryParams,
  MemoryNode,
  TransformRequest,
  TransformResult,
} from "./types.js";

import { createLlmClient } from "./lib/llm.js";
import { getStore } from "./lib/store.js";

// ─── Version (bumped on release) ─────────────────────────────────────────────

const VERSION = "0.0.0-dev";

// ─── App ─────────────────────────────────────────────────────────────────────

const app = new Hono();

// CORS: allow SPA origin (configurable via env)
app.use(
  "*",
  cors({
    origin: process.env.CORS_ORIGIN ?? "*",
    allowMethods: ["GET", "POST", "OPTIONS"],
    allowHeaders: ["Content-Type", "Authorization"],
  }),
);

// ─── Init ─────────────────────────────────────────────────────────────────────

const llm = createLlmClient();
const store = getStore();

// ─── GET /healthz ────────────────────────────────────────────────────────────

app.get("/healthz", (c: Context) => {
  return c.json({
    ok: true,
    service: "logos-cognition",
    version: VERSION,
  } satisfies HealthResponse);
});

// ─── POST /analyze ───────────────────────────────────────────────────────────

app.post("/analyze", async (c: Context) => {
  const body = await c.req.json<AnalyzeRequest>();

  // Validate input
  if (!body.content || typeof body.content !== "string") {
    return c.json({ error: "content is required (string)" }, 400);
  }
  if (body.content.length > 32000) {
    return c.json({ error: "content exceeds 32000 char limit" }, 400);
  }

  const result: AnalyzeResult = await llm.analyze(body);
  return c.json(result);
});

// ─── POST /transform ─────────────────────────────────────────────────────────

app.post("/transform", async (c: Context) => {
  const body = await c.req.json<TransformRequest>();

  // Validate
  if (!body.content || typeof body.content !== "string") {
    return c.json({ error: "content is required (string)" }, 400);
  }
  if (!body.op || !(TRANSFORM_OPS as readonly string[]).includes(body.op)) {
    return c.json(
      {
        error: `op is required, must be one of: ${TRANSFORM_OPS.join(", ")}`,
      },
      400,
    );
  }
  if (
    body.persona &&
    !(PERSONAS as readonly string[]).includes(body.persona)
  ) {
    return c.json(
      { error: `persona must be one of: ${PERSONAS.join(", ")}` },
      400,
    );
  }

  const result: TransformResult = await llm.transform(body);
  return c.json(result);
});

// ─── POST /threads/cluster ───────────────────────────────────────────────────

app.post("/threads/cluster", async (c: Context) => {
  const body = await c.req.json<ClusterRequest>();

  if (!body.message_ids || !Array.isArray(body.message_ids)) {
    return c.json({ error: "message_ids is required (string[])" }, 400);
  }
  if (!body.contents || !Array.isArray(body.contents)) {
    return c.json({ error: "contents is required (string[])" }, 400);
  }
  if (body.message_ids.length !== body.contents.length) {
    return c.json(
      { error: "message_ids and contents must have the same length" },
      400,
    );
  }
  if (body.message_ids.length === 0) {
    return c.json({
      clusters: [],
      unclustered: [],
      model: "stub",
    } satisfies ClusterResult);
  }

  // Stub: single cluster with all messages
  const result: ClusterResult = {
    clusters: [
      {
        label: "default-cluster",
        message_ids: body.message_ids,
      },
    ],
    unclustered: [],
    model: "stub",
  };

  // Persist cluster
  await store.saveThread("default-cluster", body.message_ids);

  return c.json(result);
});

// ─── GET /memory/query ───────────────────────────────────────────────────────

app.get("/memory/query", async (c: Context) => {
  const q = c.req.query("q");
  if (!q || q.trim().length === 0) {
    return c.json({ error: "q query parameter is required" }, 400);
  }

  const limit = parseInt(c.req.query("limit") ?? "10", 10);
  const nodes: MemoryNode[] = await store.queryAnchors(q, limit);

  // Stub: if no store hits, return an empty result set
  return c.json({ query: q, nodes });
});

// ─── GET /memory/list ────────────────────────────────────────────────────────

app.get("/memory/list", async (c: Context) => {
  const nodes = await store.listAnchors();
  return c.json({ nodes });
});

// ─── POST /memory/anchors ────────────────────────────────────────────────────

app.post("/memory/anchors", async (c: Context) => {
  const body = await c.req.json<AnchorRequest>();

  if (!body.content || typeof body.content !== "string") {
    return c.json({ error: "content is required (string)" }, 400);
  }

  const result: AnchorResult = await store.saveAnchor(body);
  return c.json(result, 201);
});

// ─── 404 catch-all ───────────────────────────────────────────────────────────

app.notFound((c: Context) => {
  return c.json({ error: "not found" }, 404);
});

// ─── Error handler ───────────────────────────────────────────────────────────

app.onError((err, c: Context) => {
  console.error("[cognition] unhandled error:", err);
  return c.json({ error: "internal server error" }, 500);
});

// ─── Start ────────────────────────────────────────────────────────────────────

const port = parseInt(process.env.PORT ?? "8787", 10);

console.log(`[cognition] LOGOS Cognition Service v${VERSION}`);
console.log(`[cognition] starting on port ${port}...`);

serve({ fetch: app.fetch, port });
