export function agentProgressTurnKey(userId: string, turnId: string): string {
  return `${userId}\u0000${turnId}`;
}

export const AGENT_PROGRESS_TTL_MS = 45_000;

export type AgentProgressLineView = {
  id: string;
  kind: string;
  text: string;
  toolName?: string;
  status?: string;
  finalized: boolean;
};

export type AgentProgressTurn = {
  key: string;
  turnId: string;
  userId: string;
  lines: AgentProgressLineView[];
  expiresAt: number;
};
export function updateAgentProgress(
  turns: AgentProgressTurn[],
  payload: Record<string, unknown>,
  now: number,
): AgentProgressTurn[] {
  const turnId = typeof payload.turn_id === "string" ? payload.turn_id : "";
  const op = typeof payload.op === "string" ? payload.op : "";
  if (!turnId || !op) return turns;
  const userId = typeof payload.user_id === "string" ? payload.user_id : "";
  const turnKey = agentProgressTurnKey(userId, turnId);
  if (op === "clear") {
    return turns.filter((turn) => {
      if (turn.turnId !== turnId) return true;
      return userId && turn.userId ? turn.key !== turnKey : false;
    });
  }
  const line = payload.line as Record<string, unknown> | undefined;
  const lineId = line && typeof line.id === "string" ? line.id : "";
  if (!lineId) return turns;
  const text = line && typeof line.text === "string" ? line.text : "";
  const title = line && typeof line.title === "string" ? line.title : "";
  const incomingText = text || title;
  const incomingToolName =
    line && typeof line.tool_name === "string"
      ? line.tool_name
      : typeof line?.toolName === "string"
        ? (line.toolName as string)
        : undefined;
  const incomingStatus = line && typeof line.status === "string" ? line.status : undefined;
  const incomingKind = line && typeof line.kind === "string" ? line.kind : undefined;
  // Status-only updates must retain the prior content and still finalize the line.
  const existing = turns.find((turn) => turn.key === turnKey);
  const prior = existing?.lines.find((l) => l.id === lineId);
  const view = {
    id: lineId,
    kind: incomingKind ?? prior?.kind ?? "lifecycle",
    text: incomingText || prior?.text || "",
    toolName: incomingToolName ?? prior?.toolName,
    status: incomingStatus ?? prior?.status,
    finalized: op === "finalize" || (prior?.finalized ?? false),
  };
  // Empty new lines are invisible; content-free updates to existing lines still apply.
  if (!prior && !view.text && !view.toolName) return turns;
  const expiresAt = now + AGENT_PROGRESS_TTL_MS;
  if (!existing) {
    turns = [...turns, { key: turnKey, turnId, userId, lines: [view], expiresAt }];
  } else {
    const lines = existing.lines.some((l) => l.id === lineId)
      ? existing.lines.map((l) => (l.id === lineId ? view : l))
      : [...existing.lines, view];
    turns = turns.map((turn) => (turn.key === turnKey ? { ...turn, lines, expiresAt } : turn));
  }
  return turns;
}
