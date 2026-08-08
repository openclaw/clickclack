/**
 * PROJECT LOGOS — Cognition Service Client (Semantic Surfaces)
 *
 * Typed client for the cognition service at same-origin /cognition
 * (Cloudflare worker proxies + strips /cognition prefix).
 *
 * All handlers degrade gracefully on network failure (log + return null).
 * Never throws into the UI.
 */

// ── Base URL ──

const BASE = "/cognition";

// ── Types ──

/** POST /analyze response. */
export interface AnalysisResult {
  intent?: string;
  persona?: string;
  confidence?: number;
  context_tags?: string[];
  /** Telemetry metadata from the cognition service. */
  telemetry?: Record<string, unknown>;
  /** Optional clarification question when confidence is below threshold. */
  clarification_question?: string;
}

/** POST /transform response. */
export interface TransformResult {
  transformed_content: string;
  meta?: Record<string, unknown>;
  telemetry?: Record<string, unknown>;
}

/** A single cluster from /threads/cluster. */
export interface ClusterInfo {
  id: string;
  label: string;
  message_ids: string[];
}

/** POST /threads/cluster response. */
export interface ClusterResult {
  clusters: ClusterInfo[];
  assignments: Array<{ message_id: string; cluster_id: string }>;
}

/** A memory node returned by query or anchor list. */
export interface MemoryNode {
  id: string;
  content: string;
  score: number;
}

/** POST /memory/anchors response. */
export interface AnchorResult {
  id: string;
}

export interface RespondRequest {
  content: string;
  persona?: string;
  intent?: string;
  context_messages?: Array<{ role: "user" | "assistant"; content: string }>;
  memory_hint_ids?: string[];
}

export interface RespondResult {
  content: string;
  clarification_question?: string;
  suggested_followups?: string[];
  meta: {
    intent: string;
    persona: string;
    confidence: number;
    model: string;
    latency_ms: number;
    memory_citations?: string[];
    memory_previews?: Array<{
      id: string;
      content: string;
      score?: number;
      source_message_id?: string;
      tags?: string[];
    }>;
    execution_stack: string[];
  };
}

// ── API Calls ──

/**
 * POST /analyze — classify message intent / persona / confidence.
 * Returns null on network error or non-ok response.
 */
export async function analyze(content: string): Promise<AnalysisResult | null> {
  try {
    const res = await fetch(`${BASE}/analyze`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content }),
    });
    if (!res.ok) {
      console.warn("[logos:cognition] analyze returned", res.status);
      return null;
    }
    return (await res.json()) as AnalysisResult;
  } catch (err) {
    console.error("[logos:cognition] analyze failed:", err);
    return null;
  }
}

/**
 * POST /transform — apply an inline transform op.
 * Returns null on network error or non-ok response.
 */
export async function transform(
  content: string,
  op: string,
  persona?: string,
): Promise<TransformResult | null> {
  try {
    const res = await fetch(`${BASE}/transform`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, op, persona }),
    });
    if (!res.ok) {
      console.warn("[logos:cognition] transform returned", res.status);
      return null;
    }
    return (await res.json()) as TransformResult;
  } catch (err) {
    console.error("[logos:cognition] transform failed:", err);
    return null;
  }
}

export async function respond(
  payload: RespondRequest,
): Promise<RespondResult | null> {
  try {
    const res = await fetch(`${BASE}/respond`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      console.warn("[logos:cognition] respond returned", res.status);
      return null;
    }
    return (await res.json()) as RespondResult;
  } catch (err) {
    console.error("[logos:cognition] respond failed:", err);
    return null;
  }
}

/**
 * POST /threads/cluster — semantic thread clustering.
 * Sends {message_ids, contents} to match the new cognition API.
 * Returns null on network error or non-ok response.
 */
export async function cluster(
  items: Array<{ id: string; content: string }>,
): Promise<ClusterResult | null> {
  try {
    const message_ids = items.map((m) => m.id);
    const contents = items.map((m) => m.content);
    const res = await fetch(`${BASE}/threads/cluster`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ message_ids, contents }),
    });
    if (!res.ok) {
      console.warn("[logos:cognition] cluster returned", res.status);
      return null;
    }
    return (await res.json()) as ClusterResult;
  } catch (err) {
    console.error("[logos:cognition] cluster failed:", err);
    return null;
  }
}

/**
 * GET /memory/query — semantic memory search.
 * Returns null on network error or non-ok response.
 */
export async function memoryQuery(
  q: string,
): Promise<{ nodes: MemoryNode[] } | null> {
  try {
    const params = new URLSearchParams({ q });
    const res = await fetch(`${BASE}/memory/query?${params}`);
    if (!res.ok) {
      console.warn("[logos:cognition] memory query returned", res.status);
      return null;
    }
    return (await res.json()) as { nodes: MemoryNode[] };
  } catch (err) {
    console.error("[logos:cognition] memory query failed:", err);
    return null;
  }
}

/**
 * GET /memory/anchors — list stored memory anchors.
 * Falls back to empty-nodes on any failure.
 */
export async function listMemoryAnchors(
  limit = 20,
): Promise<{ nodes: MemoryNode[] } | null> {
  try {
    const params = new URLSearchParams({ limit: String(limit) });
    const res = await fetch(`${BASE}/memory/anchors?${params}`);
    if (!res.ok) {
      console.warn("[logos:cognition] list anchors returned", res.status);
      return null;
    }
    return (await res.json()) as { nodes: MemoryNode[] };
  } catch (err) {
    console.error("[logos:cognition] list anchors failed:", err);
    return null;
  }
}

/**
 * POST /memory/anchors — pin a message as a memory anchor.
 * Returns the created anchor id or null on failure.
 */
export async function memoryAnchor(
  content: string,
  sourceMessageId?: string,
): Promise<AnchorResult | null> {
  try {
    const res = await fetch(`${BASE}/memory/anchors`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, source_message_id: sourceMessageId }),
    });
    if (!res.ok) {
      console.warn("[logos:cognition] memory anchor returned", res.status);
      return null;
    }
    return (await res.json()) as AnchorResult;
  } catch (err) {
    console.error("[logos:cognition] memory anchor failed:", err);
    return null;
  }
}
