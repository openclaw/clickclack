/**
 * COGNITIVE OS — Cognition Service Client (T4)
 *
 * Real client for the cognition service at same-origin /cognition
 * (proxied by Cloudflare worker, strips /cognition prefix).
 *
 * All handlers degrade gracefully on network failure (log + return null).
 * Never throws into the UI.
 */

import { api } from "./api";

const COGNITION_URL = import.meta.env.VITE_COGNITION_URL || "/cognition";

// ── Types (exported for consumers) ──

export type TransformOp =
  | "summarize"
  | "expand"
  | "rewrite"
  | "counterargument"
  | "alternative_framing"
  | "diagram"
  | "checklist"
  | "plan"
  | "persona_rewrite"
  | "condense"
  | "extract"
  | "invert"
  | "simulate"
  | "draft"
  | "diagnose";

export type PersonaID = "operator" | "analyst" | "creative" | "socratic" | "archivist";

export interface AnalysisResult {
  intent?: string;
  persona?: string;
  confidence?: number;
  context_tags?: string[];
  clarification_question?: string;
}

export interface TransformResult {
  original_content: string;
  transformed_content: string;
  meta?: Record<string, unknown>;
}

export interface ClusterResult {
  clusters: Array<{ id: string; label: string; message_ids: string[] }>;
  assignments: Array<{ message_id: string; cluster_id: string }>;
}

export interface MemoryNode {
  content: string;
  score: number;
}

export interface TransformHistoryEntry {
  op: string;
  at: string;
  preview: string;
}

export interface MessageMetadataPatch {
  intent?: string | null;
  persona?: string | null;
  confidence?: number | null;
  context?: Record<string, unknown> | null;
  metadata?: Record<string, unknown> | null;
  transform_history?: TransformHistoryEntry[] | null;
}

// ── Idempotence tracking (module-level, survives component remounts) ──

const analyzed = new Set<string>();

// ── Public helpers ──

/** Check if cognition service is available. */
export function cognitionAvailable(): boolean {
  return Boolean(COGNITION_URL);
}

/** Get the configured cognition API base URL. */
export function cognitionURL(): string {
  return COGNITION_URL;
}

/** Check if a message has already been analyzed (in-memory, this session). */
export function isAnalyzed(messageId: string): boolean {
  return analyzed.has(messageId);
}

/** Mark a message as analyzed so it won't be re-triggered. */
export function markAnalyzed(messageId: string): void {
  analyzed.add(messageId);
}

// ── Cognition API calls ──

/** POST /analyze — classify message intent / persona / confidence. */
export async function analyze(
  content: string,
  context?: Record<string, unknown>,
): Promise<AnalysisResult | null> {
  if (!COGNITION_URL) return null;
  try {
    const res = await fetch(`${COGNITION_URL}/analyze`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, context }),
    });
    if (!res.ok) {
      console.warn("[cognition] analyze returned", res.status);
      return null;
    }
    return (await res.json()) as AnalysisResult;
  } catch (err) {
    console.error("[cognition] analyze failed:", err);
    return null;
  }
}

/** POST /transform — apply an inline transform op. */
export async function transform(
  content: string,
  op: TransformOp,
  persona?: PersonaID,
): Promise<TransformResult | null> {
  if (!COGNITION_URL) return null;
  try {
    const res = await fetch(`${COGNITION_URL}/transform`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, op, persona }),
    });
    if (!res.ok) {
      console.warn("[cognition] transform returned", res.status);
      return null;
    }
    return (await res.json()) as TransformResult;
  } catch (err) {
    console.error("[cognition] transform failed:", err);
    return null;
  }
}

/** POST /threads/cluster — semantic thread clustering. */
export async function cluster(contents: string[]): Promise<ClusterResult | null> {
  if (!COGNITION_URL) return null;
  try {
    const res = await fetch(`${COGNITION_URL}/threads/cluster`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ contents }),
    });
    if (!res.ok) {
      console.warn("[cognition] cluster returned", res.status);
      return null;
    }
    return (await res.json()) as ClusterResult;
  } catch (err) {
    console.error("[cognition] cluster failed:", err);
    return null;
  }
}

/** GET /memory/query — semantic memory search. */
export async function memoryQuery(q: string): Promise<{ nodes: MemoryNode[] } | null> {
  if (!COGNITION_URL) return null;
  try {
    const params = new URLSearchParams({ q });
    const res = await fetch(`${COGNITION_URL}/memory/query?${params}`);
    if (!res.ok) {
      console.warn("[cognition] memory query returned", res.status);
      return null;
    }
    return (await res.json()) as { nodes: MemoryNode[] };
  } catch (err) {
    console.error("[cognition] memory query failed:", err);
    return null;
  }
}

/** POST /memory/anchors — pin a message as a memory anchor. */
export async function memoryAnchor(
  content: string,
  source_message_id?: string,
): Promise<{ id: string } | null> {
  if (!COGNITION_URL) return null;
  try {
    const res = await fetch(`${COGNITION_URL}/memory/anchors`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, source_message_id }),
    });
    if (!res.ok) {
      console.warn("[cognition] memory anchor returned", res.status);
      return null;
    }
    return (await res.json()) as { id: string };
  } catch (err) {
    console.error("[cognition] memory anchor failed:", err);
    return null;
  }
}

// ── Message metadata patching (main API, not cognition service) ──

/** PATCH /api/messages/{id}/metadata — persist cognitive metadata. */
export async function patchMessageMetadata(
  messageId: string,
  metadata: MessageMetadataPatch,
): Promise<boolean> {
  try {
    await api(`/api/messages/${messageId}/metadata`, {
      method: "PATCH",
      body: JSON.stringify(metadata),
    });
    return true;
  } catch (err) {
    console.error("[cognition] patch metadata failed:", err);
    return false;
  }
}

// ── Composite helpers ──

/** Analyze message content and persist intent/persona/confidence via API. */
export async function analyzeAndPersist(
  messageId: string,
  content: string,
): Promise<AnalysisResult | null> {
  if (!COGNITION_URL) return null;
  if (isAnalyzed(messageId)) return null;
  markAnalyzed(messageId);

  const result = await analyze(content);
  if (!result) return null;

  await patchMessageMetadata(messageId, {
    intent: result.intent ?? null,
    persona: result.persona ?? null,
    confidence: result.confidence ?? null,
    context: result.context_tags ? { tags: result.context_tags } : null,
  });

  return result;
}

// ── Legacy stubs kept for backward compat (MessageUtilities T1 path) ──

let _actionQueue: Array<{
  kind: string;
  messageId: string;
  payload?: string;
}> = [];

/** Queue a utility action. Kept for backward compat but deprecated in T4. */
export function queueAction(action: {
  kind: string;
  messageId: string;
  payload?: string;
}): void {
  _actionQueue.push(action);
}

/** Drain queued actions. */
export function drainQueue(): Array<{
  kind: string;
  messageId: string;
  payload?: string;
}> {
  return _actionQueue.splice(0, _actionQueue.length);
}
