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
  context?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface TransformHistoryEntry {
  op: string;
  at: string;
  preview: string;
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
