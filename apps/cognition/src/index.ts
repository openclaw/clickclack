// PROJECT LOGOS — Cognition Service: HTTP entry point
//
// Hono server exposing the cognition API.
// Routes: health, analyze, transform, thread clustering, memory anchors/query.
//
// Intelligence wired via LlmClient (DeepSeek/OpenAI/stub).
// Embeddings via EmbedProvider (local transformers.js / OpenAI / stub).

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
  ClusterAssignment,
  ClusterRequest,
  ClusterResult,
  HealthResponse,
  MemoryNode,
  RespondRequest,
  RespondResult,
  Telemetry,
  TransformRequest,
  TransformResult,
} from "./types.js";

import { createLlmClient } from "./lib/llm.js";
import { createEmbedProvider } from "./lib/embed.js";
import { getStore, cosineSimilarity } from "./lib/store.js";
import { handleRespond } from "./lib/respond.js";

// ─── Version (bumped on release) ─────────────────────────────────────────────

const VERSION = "0.0.0-dev";

// ─── App ─────────────────────────────────────────────────────────────────────

const app = new Hono();

// PROJECT LOGOS hardening: optional shared-secret gate. When LOGOS_AUTH_TOKEN
// is set, every request must carry it as `Authorization: Bearer <token>` or
// `X-Logos-Token`. The Cloudflare worker injects it on proxied /cognition/*
// calls, so browsers never see it and arbitrary internet callers get 401.
const AUTH_TOKEN = process.env.LOGOS_AUTH_TOKEN ?? "";

app.use("*", async (c, next) => {
  if (!AUTH_TOKEN) {
    await next();
    return;
  }
  const auth = c.req.header("authorization") ?? "";
  const bearer = auth.startsWith("Bearer ") ? auth.slice(7) : "";
  const header = c.req.header("x-logos-token") ?? "";
  if (bearer !== AUTH_TOKEN && header !== AUTH_TOKEN) {
    return c.json({ error: "unauthorized" }, 401);
  }
  await next();
});

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
const embedProvider = createEmbedProvider();
const store = getStore();

// Wire embed provider to store (replaces old setLlmClient)
store.setEmbedProvider(embedProvider);

// Warmup: kick off model download in background so first real request isn't slow
embedProvider.warmup().then(() => {
  console.log("[cognition] embed warmup complete");
}).catch((err) => {
  console.warn("[cognition] embed warmup failed:", err);
});

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

  // Enrich telemetry with memory citations (when embeddings are real)
  if (embedProvider.isReal && result.telemetry) {
    try {
      const nodes = await store.queryAnchors(body.content, 3);
      const citationIds = nodes
        .filter((n) => (n.score ?? 0) > 0.4)
        .map((n) => n.id);
      if (citationIds.length > 0) {
        result.telemetry = {
          ...result.telemetry,
          memory_citations: citationIds,
          execution_stack: [
            ...result.telemetry.execution_stack,
            "embed_query()",
          ],
        };
      }
    } catch (err) {
      console.warn("[cognition] memory citation enrichment failed:", err);
    }
  }

  return c.json(result);
});

// ─── POST /respond ───────────────────────────────────────────────────────────

app.post("/respond", async (c: Context) => {
  const body = await c.req.json<RespondRequest>();

  // Validate input
  if (!body.content || typeof body.content !== "string") {
    return c.json({ error: "content is required (string)" }, 400);
  }
  if (body.content.length > 32000) {
    return c.json({ error: "content exceeds 32000 char limit" }, 400);
  }
  if (
    body.intent &&
    !(INTENTS as readonly string[]).includes(body.intent)
  ) {
    return c.json(
      { error: `intent must be one of: ${INTENTS.join(", ")} or omitted` },
      400,
    );
  }
  if (
    body.persona &&
    !(PERSONAS as readonly string[]).includes(body.persona)
  ) {
    return c.json(
      { error: `persona must be one of: ${PERSONAS.join(", ")} or omitted` },
      400,
    );
  }
  if (
    body.context_messages &&
    (!Array.isArray(body.context_messages) ||
      body.context_messages.length > 20)
  ) {
    return c.json(
      { error: "context_messages must be an array of up to 20 messages" },
      400,
    );
  }

  const result: RespondResult = await handleRespond(body, llm, store);
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
      model: embedProvider.isReal ? embedProvider.provider : "stub",
      assignments: [],
    } satisfies ClusterResult);
  }

  // Single message: trivial cluster
  if (body.message_ids.length === 1) {
    const label = `cluster-0`;
    await store.saveThread(label, body.message_ids);
    const assignments: ClusterAssignment[] = [
      {
        message_id: body.message_ids[0],
        cluster_id: 0,
        centroid_similarity: 1.0,
      },
    ];
    return c.json({
      clusters: [{ label, message_ids: body.message_ids }],
      unclustered: [],
      model: embedProvider.isReal ? embedProvider.provider : "stub",
      assignments,
    } satisfies ClusterResult);
  }

  try {
    // Generate embeddings for all messages using embedProvider
    const embeddings: number[][] = [];
    for (const content of body.contents) {
      const emb = await embedProvider.embed(content);
      embeddings.push(emb);
    }

    // Check if embeddings are stubs (all zeros)
    const isStub =
      embeddings.length === 0 ||
      embeddings.every(
        (e) => e.length === 0 || e.every((v) => v === 0),
      );

    if (isStub) {
      return fallbackCluster(body);
    }

    // Cosine similarity matrix
    const n = body.contents.length;
    const similarityMatrix: number[][] = Array.from({ length: n }, () =>
      new Array(n).fill(0),
    );

    for (let i = 0; i < n; i++) {
      for (let j = i; j < n; j++) {
        const sim = cosineSimilarity(embeddings[i], embeddings[j]);
        similarityMatrix[i][j] = sim;
        similarityMatrix[j][i] = sim;
      }
    }

    // Connected-components clustering with similarity threshold.
    // all-MiniLM-L6-v2 scores tighter than OpenAI embeddings: related short
    // texts land ~0.34-0.44, unrelated <=0.17. 0.55 (OpenAI-space) would
    // cluster nothing. Measured 2026-08-07 against live model.
    const SIMILARITY_THRESHOLD = 0.25;
    const visited = new Array(n).fill(false);
    const clusterGroups: number[][] = [];

    for (let i = 0; i < n; i++) {
      if (visited[i]) continue;
      const cluster: number[] = [];
      const queue = [i];
      visited[i] = true;

      while (queue.length > 0) {
        const current = queue.shift()!;
        cluster.push(current);
        for (let j = 0; j < n; j++) {
          if (!visited[j] && similarityMatrix[current][j] >= SIMILARITY_THRESHOLD) {
            visited[j] = true;
            queue.push(j);
          }
        }
      }
      clusterGroups.push(cluster);
    }

    // Build cluster labels and assignments
    const clusters: ClusterResult["clusters"] = [];
    const assignments: ClusterAssignment[] = [];
    const unclustered: string[] = [];

    for (let ci = 0; ci < clusterGroups.length; ci++) {
      const group = clusterGroups[ci];
      const mids = group.map((idx) => body.message_ids[idx]);
      const label = `cluster-${ci}`;

      if (group.length === 1) {
        unclustered.push(body.message_ids[group[0]]);
        assignments.push({
          message_id: body.message_ids[group[0]],
          cluster_id: -1,
          centroid_similarity: 1.0,
        });
      } else {
        clusters.push({ label, message_ids: mids });
        await store.saveThread(label, mids);

        for (const idx of group) {
          // Average similarity to all other members in the cluster
          const others = group.filter((o) => o !== idx);
          const avgSim =
            others.length > 0
              ? others.reduce((sum, o) => sum + similarityMatrix[idx][o], 0) /
                others.length
              : 1.0;
          assignments.push({
            message_id: body.message_ids[idx],
            cluster_id: ci,
            centroid_similarity: Math.round(avgSim * 1000) / 1000,
          });
        }
      }
    }

    return c.json({
      clusters,
      unclustered,
      model: embedProvider.provider,
      assignments,
    } satisfies ClusterResult);
  } catch (err) {
    console.warn("[cognition] clustering failed, falling back to stub:", err);
    return fallbackCluster(body);
  }
});

/** Fallback: all messages in one cluster (original stub behavior) */
async function fallbackCluster(body: ClusterRequest): Promise<Response> {
  const assignments: ClusterAssignment[] = body.message_ids.map((mid) => ({
    message_id: mid,
    cluster_id: 0,
    centroid_similarity: 1.0,
  }));

  await getStore().saveThread("default-cluster", body.message_ids);

  return new Response(
    JSON.stringify({
      clusters: [{ label: "default-cluster", message_ids: body.message_ids }],
      unclustered: [],
      model: "stub",
      assignments,
    } satisfies ClusterResult),
    {
      status: 200,
      headers: { "Content-Type": "application/json" },
    },
  );
}

// ─── GET /memory/query ───────────────────────────────────────────────────────

app.get("/memory/query", async (c: Context) => {
  const q = c.req.query("q");
  if (!q || q.trim().length === 0) {
    return c.json({ error: "q query parameter is required" }, 400);
  }

  const limit = parseInt(c.req.query("limit") ?? "10", 10);
  const nodes: MemoryNode[] = await store.queryAnchors(q, limit);

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
