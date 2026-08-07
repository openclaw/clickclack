/**
 * COGNITIVE OS — Cognition Service Client Stub (T1)
 *
 * The cognition service (apps/cognition) is NOT live yet (T3).
 * All handlers here log/queue; the markers render as absent states
 * when metadata fields are missing.
 *
 * Cognition API base is configurable via VITE_COGNITION_URL.
 * Default '' means no backend — all actions are no-ops.
 */

const COGNITION_URL = import.meta.env.VITE_COGNITION_URL || "/cognition";

type TransformOp =
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

type PersonaID = "operator" | "analyst" | "creative" | "socratic" | "archivist";

interface TransformRequest {
  messageId: string;
  content: string;
  op: TransformOp;
  persona?: PersonaID;
}

interface UtilityAction {
  kind: "transform" | "summarize" | "expand" | "thread_link" | "memory_link" | "persona_switch";
  messageId: string;
  payload?: string;
}

const actionQueue: UtilityAction[] = [];

/** Check if cognition service is available. */
export function cognitionAvailable(): boolean {
  return Boolean(COGNITION_URL);
}

/** Get the configured cognition API base URL. */
export function cognitionURL(): string {
  return COGNITION_URL;
}

/** Queue a utility action for later processing (T4 integration). */
export function queueAction(action: UtilityAction): void {
  actionQueue.push(action);
  if (COGNITION_URL) {
    console.debug("[cognition] queued action:", action.kind, action.messageId);
  }
}

/** Drain and return all queued actions. */
export function drainQueue(): UtilityAction[] {
  return actionQueue.splice(0, actionQueue.length);
}

/** Stub: request a transform. In T4 this will POST to the cognition service. */
export async function requestTransform(req: TransformRequest): Promise<string | null> {
  if (!COGNITION_URL) {
    console.debug("[cognition] transform stub:", req.op, req.messageId);
    return null;
  }
  try {
    const res = await fetch(`${COGNITION_URL}/transform`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
    });
    if (!res.ok) return null;
    const data = await res.json();
    return data.content ?? null;
  } catch (err) {
    console.error("[cognition] transform failed:", err);
    return null;
  }
}

/** Stub: request analysis of message content. */
export async function requestAnalysis(
  messageId: string,
  content: string,
): Promise<{ intent?: string; persona?: string; confidence?: number } | null> {
  if (!COGNITION_URL) {
    console.debug("[cognition] analyze stub:", messageId);
    return null;
  }
  try {
    const res = await fetch(`${COGNITION_URL}/analyze`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ message_id: messageId, content }),
    });
    if (!res.ok) return null;
    return await res.json();
  } catch (err) {
    console.error("[cognition] analyze failed:", err);
    return null;
  }
}
