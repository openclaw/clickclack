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
  /** Ambiguity clarification question (present when confidence < threshold or LLM flags ambiguity) */
  clarification_question?: string;
  /** Deep-inspection telemetry (latency, tokens, execution stack, memory citations) */
  telemetry: Telemetry;
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
  /** Extended metadata about the transform execution */
  meta: {
    op: TransformOp;
    model: string;
    confidence: number;
    tokens?: number;
  };
  /** Deep-inspection telemetry (latency, tokens, execution stack) */
  telemetry: Telemetry;
}

/** POST /threads/cluster request body */
export interface ClusterRequest {
  message_ids: string[];
  contents: string[];
}

/** Per-message cluster assignment */
export interface ClusterAssignment {
  message_id: string;
  cluster_id: number;
  centroid_similarity: number;
}

/** POST /threads/cluster response */
export interface ClusterResult {
  clusters: {
    label: string;
    message_ids: string[];
  }[];
  unclustered: string[];
  model: string;
  /** Per-message assignment details with similarity scores */
  assignments: ClusterAssignment[];
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

// ─── Telemetry (LOGOS_SPEC §8.5 deep-inspection) ────────────────────────────

export interface Telemetry {
  /** Wall-clock latency of the LLM call in milliseconds */
  latency_ms: number;
  /** Total tokens consumed (from API usage metadata, or estimated chars/4) */
  total_tokens?: number;
  /** Model identifier used for this request */
  model: string;
  /** Derived confidence score 0-1 (from analyze confidence, or heuristics) */
  intent_vector_score?: number;
  /** Top-k memory node IDs relevant to the input (when embeddings available) */
  memory_citations?: string[];
  /** Steps actually executed during processing */
  execution_stack: string[];
  /** Per-token log probabilities (only when API returns them; DeepSeek does not) */
  logprobs?: { token: string; prob: number }[];
}

// ─── Adaptive Response Generator (§6.1) ─────────────────────────────────────

/** POST /respond request body */
export interface RespondRequest {
  content: string;
  /** Requested persona (defaults to operator if omitted) */
  persona?: Persona;
  /** Pre-classified intent (if omitted, runs intent parser) */
  intent?: Intent;
  /** Recent conversation context for the LLM */
  context_messages?: { role: "user" | "assistant"; content: string }[];
  /** Related memory anchor ids the caller wants weighted in the reply. */
  memory_hint_ids?: string[];
}

/** POST /respond response */
export interface RespondResult {
  /** The generated companion reply */
  content: string;
  /** Clarifying question surfaced when the input is ambiguous. */
  clarification_question?: string;
  /** Short operational follow-ups the UI can surface beside the reply. */
  suggested_followups?: string[];
  meta: {
    intent: Intent;
    persona: Persona;
    confidence: number;
    model: string;
    latency_ms: number;
    /** Top-k memory node IDs relevant to the input (ask/command intents only) */
    memory_citations?: string[];
    /** Preview snippets from the memory nodes actually pulled into context. */
    memory_previews?: Array<{
      id: string;
      content: string;
      score?: number;
      source_message_id?: string;
      tags?: string[];
    }>;
    /** Steps actually executed in order */
    execution_stack: string[];
  };
}

// ─── Health ───────────────────────────────────────────────────────────────────

export interface HealthResponse {
  ok: true;
  service: "logos-cognition";
  version: string;
}
