// PROJECT LOGOS — Cognition Service: type definitions
// Authoritative types for the message object schema and all cognition contracts.

// ─── Message Object (LOGOS_SPEC.md §6.2) ─────────────────────────────────────

export interface MessageObject {
  content: string;
  intent: Intent;
  persona: Persona;
  context: unknown; // object or string
  thread_id: string;
  confidence: number; // 0.0 – 1.0
  metadata: Record<string, unknown>;
  transform_history: TransformRecord[];
}

export interface TransformRecord {
  op: TransformOp;
  persona?: Persona;
  timestamp: string; // ISO 8601
  result_preview: string; // first 120 chars of transformed content
}

// ─── Intent union (LOGOS.md — "Intents") ─────────────────────────────────────
export const INTENTS = ["ask", "command", "reflect", "draft", "clarify", "explore"] as const;
export type Intent = (typeof INTENTS)[number];

// ─── Persona union (LOGOS.md — "Personas") ────────────────────────────────────
export const PERSONAS = [
  "operator",
  "analyst",
  "creative",
  "socratic",
  "archivist",
] as const;
export type Persona = (typeof PERSONAS)[number];

// ─── Transform ops (LOGOS.md — "Inline Transform Ops") ───────────────────────
export const TRANSFORM_OPS = [
  "summarize",
  "expand",
  "rewrite",
  "counterargument",
  "alternative_framing",
  "diagram",
  "checklist",
  "plan",
  "persona_rewrite",
  "condense",
  "extract",
  "invert",
  "simulate",
  "draft",
  "diagnose",
] as const;
export type TransformOp = (typeof TRANSFORM_OPS)[number];

// ─── Route contracts ─────────────────────────────────────────────────────────

/** POST /analyze request body */
export interface AnalyzeRequest {
  content: string;
  context?: string;
}

/** POST /analyze response */
export interface AnalyzeResult {
  intent: Intent;
  persona: Persona;
  confidence: number;
  context_tags: string[];
  model: string;
}

/** POST /transform request body */
export interface TransformRequest {
  content: string;
  op: TransformOp;
  persona?: Persona;
}

/** POST /transform response */
export interface TransformResult {
  original_content: string;
  transformed_content: string;
  op: TransformOp;
  persona: Persona;
  confidence: number;
  model: string;
}

/** POST /threads/cluster request body */
export interface ClusterRequest {
  message_ids: string[];
  contents: string[];
}

/** POST /threads/cluster response */
export interface ClusterResult {
  clusters: {
    label: string;
    message_ids: string[];
  }[];
  unclustered: string[];
  model: string;
}

/** GET /memory/query response node */
export interface MemoryNode {
  id: string;
  content: string;
  source_message_id?: string;
  created_at: string;
  tags: string[];
  score?: number; // relevance score (only present for query results)
}

/** GET /memory/query request query params */
export interface MemoryQueryParams {
  q: string;
  limit?: number;
}

/** POST /memory/anchors request body */
export interface AnchorRequest {
  content: string;
  source_message_id?: string;
  tags?: string[];
}

/** POST /memory/anchors response */
export interface AnchorResult {
  id: string;
  content: string;
  source_message_id?: string;
  created_at: string;
  tags: string[];
}

// ─── Health ───────────────────────────────────────────────────────────────────

export interface HealthResponse {
  ok: true;
  service: "logos-cognition";
  version: string;
}
