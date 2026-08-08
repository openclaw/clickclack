// PROJECT LOGOS — minimal clickclack substrate types
// Re-export SDK types where available; declare structural types for LOGOS extensions.

import type { ClickClackClient } from "@clickclack/sdk-ts";

// Re-export key types from the SDK (avoid re-declaring what the SDK already owns).
export type {
  Workspace,
  Channel,
  Message,
  MessagePage,
  User,
  RealtimeEvent,
} from "@clickclack/sdk-ts";

// ---------------------------------------------------------------------------
// LOGOS-specific message metadata extensions (spec §8.4)
// These fields are stored on the message object via PATCH /api/messages/{id}/metadata
// and surfaced in the ChatStream mono metadata header line.
// ---------------------------------------------------------------------------

export interface MessageMetadata {
  intent?: string; // "ask" | "command" | "reflect" | "draft" | "clarify" | "explore"
  persona?: string; // "operator" | "analyst" | "creative" | "socratic" | "archivist"
  confidence?: number; // 0-1 classifier score
  thread_id?: string; // semantic thread id e.g. "#SYS-LOG-042"
  latency_ms?: number; // execution latency
  transform_history?: TransformHistoryEntry[];
  clarification_question?: string;
  telemetry?: Record<string, unknown>;
  context?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface TransformHistoryEntry {
  op: string;
  at: string;
  preview: string;
  persona?: string;
  model?: string;
}

export type CognitiveMessage = import("@clickclack/sdk-ts").Message & {
  intent?: string;
  persona?: string;
  confidence?: number;
  context?: unknown;
  metadata?: unknown;
  transform_history?: unknown;
};

function parseJSONValue(value: unknown): unknown {
  if (typeof value !== "string") return value;
  const trimmed = value.trim();
  if (!trimmed) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
    return value;
  }
}

function parseRecord(value: unknown): Record<string, unknown> {
  const parsed = parseJSONValue(value);
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    return { ...(parsed as Record<string, unknown>) };
  }
  return {};
}

function normalizeTransformHistory(value: unknown): TransformHistoryEntry[] {
  const parsed = parseJSONValue(value);
  if (!Array.isArray(parsed)) return [];
  return parsed.flatMap((entry): TransformHistoryEntry[] => {
    if (!entry || typeof entry !== "object") return [];
    const record = entry as Record<string, unknown>;
    const op = typeof record.op === "string" ? record.op : "";
    if (!op) return [];
    const at = typeof record.at === "string"
      ? record.at
      : typeof record.timestamp === "string"
        ? record.timestamp
        : "";
    const preview = typeof record.preview === "string"
      ? record.preview
      : typeof record.result_preview === "string"
        ? record.result_preview
        : "";
    return [{
      op,
      at,
      preview,
      ...(typeof record.persona === "string" ? { persona: record.persona } : {}),
      ...(typeof record.model === "string" ? { model: record.model } : {}),
    }];
  });
}

export function readMessageMetadata(message: Record<string, unknown>): MessageMetadata {
  const metadata = parseRecord(message.metadata) as MessageMetadata;

  if (!metadata.intent && typeof message.intent === "string" && message.intent) {
    metadata.intent = message.intent;
  }
  if (!metadata.persona && typeof message.persona === "string" && message.persona) {
    metadata.persona = message.persona;
  }
  if (metadata.confidence === undefined && typeof message.confidence === "number") {
    metadata.confidence = message.confidence;
  }

  const context = parseJSONValue(message.context);
  if (context && typeof context === "object" && !Array.isArray(context)) {
    metadata.context = context as Record<string, unknown>;
  } else if (Array.isArray(context)) {
    metadata.context = { tags: context.map(String) };
  }

  const transformHistory = normalizeTransformHistory(message.transform_history);
  if (transformHistory.length > 0) {
    metadata.transform_history = transformHistory;
  }

  const telemetry = parseRecord(metadata.telemetry);
  if (Object.keys(telemetry).length > 0) {
    metadata.telemetry = telemetry;
    if (
      metadata.latency_ms === undefined &&
      typeof telemetry.latency_ms === "number"
    ) {
      metadata.latency_ms = telemetry.latency_ms;
    }
  }

  return metadata;
}

// ---------------------------------------------------------------------------
// Chat store state shape
// ---------------------------------------------------------------------------

export type ChatStatus = "booting" | "ready" | "error";

export interface ChatStateSnapshot {
  status: ChatStatus;
  workspaces: import("@clickclack/sdk-ts").Workspace[];
  channels: import("@clickclack/sdk-ts").Channel[];
  activeWorkspaceId: string | null;
  activeChannelId: string | null;
  messages: import("@clickclack/sdk-ts").Message[];
  error?: string;
  user?: import("@clickclack/sdk-ts").User;
  /** Active realtime transport: WebSocket or polling fallback. */
  realtime: "ws" | "poll";
}

// ---------------------------------------------------------------------------
// Auth session
// ---------------------------------------------------------------------------

export interface Session {
  user: import("@clickclack/sdk-ts").User;
  token?: string; // present when using Bearer token auth
}

// ---------------------------------------------------------------------------
// SDK client instance (imported type, not re-exported — used internally)
// ---------------------------------------------------------------------------

export type CCClient = ClickClackClient;
