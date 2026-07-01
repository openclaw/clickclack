// Render-time coalescing of durable agent activity rows into one preamble block
// per agent turn.
//
// The server stores agent narration as flat, individual rows in the messages
// table: kind="agent_commentary" for prose snapshots and kind="agent_tool" for
// tool calls, all sharing a turn_id within one agent turn (set by the bridge).
// Rendered one-row-per-message that reads as noise: a dozen badged rows per
// turn. This module collapses each turn's activity rows into a single
// synthetic "preamble" message whose block interleaves commentary prose and
// tool calls in arrival order (commentary, tool, commentary, tool...), the
// same chronological step timeline clickglass renders.
//
// Grouping is by turn_id across the whole list, not by adjacency. The bridge
// flushes trailing commentary on a debounce, so the agent's final answer (an
// ordinary kind="message" row) can land BETWEEN two activity rows of the same
// turn. Adjacency grouping splits that into two blocks and leaves the trailing
// fragment permanently expanded under the final answer, which reads as the
// final message being swallowed by the preamble. Instead, every activity row
// of a turn folds into one block anchored at the turn's first activity row, so
// the block always renders above the final answer as one unit.
//
// A turn is final (collapse to one line) when its narration is over:
//   - a normal message from the same author (the final answer) follows the
//     turn's first activity row, or
//   - any message at all follows the turn's last activity row, or
//   - the turn's newest activity row is stale (no new frames for a few
//     minutes), covering turns whose final answer never landed as a durable
//     row (it went to another surface, or the bridge restarted); without
//     this such turns would render as LIVE and expanded forever.
// While the turn's rows are the newest content in the channel it is live.
//
// Two independent operator flags control visibility:
//   hideCommentary  - drop the prose items from the block
//   hideToolCalls   - drop the tool-call items from the block
// With both set, the block is omitted entirely (no synthetic row emitted).

import type { Message, PreambleBlock, PreambleItem } from "../types";

export type {
  PreambleBlock,
  PreambleCommentaryItem,
  PreambleItem,
  PreambleToolItem,
} from "../types";

export type AgentActivityFlags = {
  hideCommentary: boolean;
  hideToolCalls: boolean;
};

const ACTIVITY_KINDS = new Set(["agent_commentary", "agent_tool"]);

// A turn with no new activity rows for this long is treated as finished even
// when no later message proves it. Generous: real turns emit frames at least
// every debounce interval (~seconds) while running.
const TURN_STALE_MS = 3 * 60 * 1000;

export function isAgentActivity(message: Message): boolean {
  return message.kind !== undefined && ACTIVITY_KINDS.has(message.kind);
}

function isOrdinaryMessage(message: Message): boolean {
  return message.kind === undefined || message.kind === "message";
}

function authorKey(message: Message): string {
  return message.author?.id || message.author_id || "";
}

function turnKey(message: Message): string {
  const scope = message.channel_id
    ? `channel:${message.channel_id}`
    : `direct:${message.direct_conversation_id || ""}`;
  return `${scope}\u0000${authorKey(message)}\u0000${message.turn_id || message.id}`;
}

// Parse a stored activity body into a tool name + optional detail. The bridge
// writes tool rows as "**head**\n\ndetail", "**head**", or a bare string. The
// head is often a coalesced step chain ("command print text -> run ps -> ...");
// to avoid surfacing the full chain (which reads as noise), we take the first
// token of the head as the tool name and fold the remainder into the detail,
// which renders as a single ellipsis-truncated line.
function parseToolBody(body: string): { name: string; detail?: string } {
  const trimmed = body.trim();
  let head = "";
  let text = "";
  const withText = trimmed.match(/^\*\*([^*]+)\*\*\s*\n+([\s\S]+)$/);
  const headOnly = trimmed.match(/^\*\*([^*]+)\*\*$/);
  if (withText) {
    head = withText[1].trim();
    text = collapseWhitespace(withText[2]);
  } else if (headOnly) {
    head = headOnly[1].trim();
  } else {
    // No well-formed bold head (e.g. an unclosed "**exec ..." from older
    // bridge data). Strip stray bold markers and still split a leading verb.
    const cleaned = trimmed.replace(/\*\*/g, "").trim();
    return splitHead(cleaned, "");
  }
  return splitHead(head, text);
}

// Split a head into a leading tool-name token and a folded detail. The first
// whitespace-delimited word is treated as the tool verb (command, exec, read,
// message); the rest of the head, plus any body text, becomes the detail.
function splitHead(head: string, text: string): { name: string; detail?: string } {
  const collapsedHead = collapseWhitespace(head);
  const spaceIdx = collapsedHead.indexOf(" ");
  let name: string;
  let rest: string;
  if (spaceIdx === -1) {
    name = collapsedHead;
    rest = "";
  } else {
    name = collapsedHead.slice(0, spaceIdx);
    rest = collapsedHead.slice(spaceIdx + 1).trim();
  }
  const detailParts = [rest, text].filter((p) => p.length > 0);
  const detail = detailParts.join(" · ");
  return { name, detail: detail || undefined };
}

function collapseWhitespace(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

// Build the ordered item list for one turn's activity rows. Returns null when
// the flags suppress every visible item.
function buildBlock(
  turnId: string,
  rows: Message[],
  final: boolean,
  flags: AgentActivityFlags,
): PreambleBlock | null {
  const items: PreambleItem[] = [];
  for (const row of rows) {
    if (row.kind === "agent_tool") {
      if (flags.hideToolCalls) continue;
      const parsed = parseToolBody(row.body);
      items.push({
        type: "tool",
        id: row.id,
        name: parsed.name,
        detail: parsed.detail,
        full: row.body.trim(),
      });
    } else {
      // agent_commentary
      if (flags.hideCommentary) continue;
      const body = row.body.trim();
      if (body) items.push({ type: "commentary", id: row.id, body });
    }
  }
  if (items.length === 0) return null;
  return { turnId, items, final };
}

type TurnAccumulator = {
  turnId: string;
  rows: Message[];
  firstIndex: number;
  lastIndex: number;
  author: string;
};

// Walk an ordered message list and collapse each turn's agent activity rows
// (grouped by turn_id across the whole list) into a single synthetic preamble
// message anchored at the turn's first activity row. Ordinary messages pass
// through untouched and keep their order.
export function coalesceAgentActivity(
  messages: Message[],
  flags: AgentActivityFlags,
  now = Date.now(),
): Message[] {
  const turns = new Map<string, TurnAccumulator>();
  const lastOrdinaryIndexByAuthor = new Map<string, number>();
  for (let i = 0; i < messages.length; i += 1) {
    const message = messages[i];
    if (!isAgentActivity(message)) {
      if (isOrdinaryMessage(message)) lastOrdinaryIndexByAuthor.set(authorKey(message), i);
      continue;
    }
    const key = turnKey(message);
    const turn = turns.get(key);
    if (turn) {
      turn.rows.push(message);
      turn.lastIndex = i;
    } else {
      turns.set(key, {
        turnId: message.turn_id || message.id,
        rows: [message],
        firstIndex: i,
        lastIndex: i,
        author: authorKey(message),
      });
    }
  }
  if (turns.size === 0) return messages;

  // Decide turn finality: a same-author ordinary message (the final answer)
  // after the turn opened, anything after the turn's last activity row, or
  // staleness (no new frames for TURN_STALE_MS).
  const finals = new Map<string, boolean>();
  for (const [key, turn] of turns) {
    let final =
      turn.lastIndex < messages.length - 1 ||
      (lastOrdinaryIndexByAuthor.get(turn.author) ?? -1) > turn.firstIndex;
    if (!final) {
      const newest = Date.parse(turn.rows[turn.rows.length - 1].created_at);
      if (Number.isFinite(newest) && now - newest > TURN_STALE_MS) final = true;
    }
    finals.set(key, final);
  }

  const out: Message[] = [];
  for (let i = 0; i < messages.length; i += 1) {
    const message = messages[i];
    if (!isAgentActivity(message)) {
      out.push(message);
      continue;
    }
    const key = turnKey(message);
    const turn = turns.get(key);
    if (!turn || turn.firstIndex !== i) continue; // folded into the anchor row
    const block = buildBlock(turn.turnId, turn.rows, finals.get(key) === true, flags);
    if (!block) continue;
    // Synthesize one row from the turn's first activity row so author,
    // timestamp, channel/seq, and turn_id flow through grouping and the
    // virtualizer unchanged. The body is cleared; preamble_block drives
    // rendering.
    out.push({
      ...turn.rows[0],
      kind: "agent_commentary",
      body: "",
      attachments: undefined,
      quoted_message_id: undefined,
      preamble_block: block,
    });
  }
  return out;
}
