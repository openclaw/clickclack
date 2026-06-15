// Render-time coalescing of durable agent activity rows into one preamble block
// per agent turn.
//
// The server stores agent narration as flat, individual rows in the messages
// table: kind="agent_commentary" for prose snapshots and kind="agent_tool" for
// tool calls, all sharing a turn_id within one agent turn (set by the bridge).
// Rendered one-row-per-message that reads as noise: a dozen badged rows per
// turn. This module collapses each maximal run of consecutive same-turn
// activity rows into a single synthetic "preamble" message that the transcript
// renders as one block: incrementing commentary prose plus a collapsed-by-
// default, expandable list of tool calls. When the agent's turn ends (any
// message follows the run), the block is marked final so the UI can collapse it
// to a single line.
//
// Two independent operator flags control visibility:
//   hideCommentary  - drop the prose from the block
//   hideToolCalls   - drop the tool-call sub-items from the block
// With both set, the block is omitted entirely (no synthetic row emitted).

import type { Message, PreambleBlock, PreambleToolItem } from "../types";

export type { PreambleBlock, PreambleToolItem } from "../types";

export type AgentActivityFlags = {
  hideCommentary: boolean;
  hideToolCalls: boolean;
};

const ACTIVITY_KINDS = new Set(["agent_commentary", "agent_tool"]);

export function isAgentActivity(message: Message): boolean {
  return message.kind !== undefined && ACTIVITY_KINDS.has(message.kind);
}

function turnKey(message: Message): string {
  return message.turn_id || message.id;
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

// Build the preamble block for one run of activity rows. Returns null when the
// flags suppress every visible part of the block.
function buildBlock(
  turnId: string,
  run: Message[],
  final: boolean,
  flags: AgentActivityFlags,
): PreambleBlock | null {
  const proseParts: string[] = [];
  const tools: PreambleToolItem[] = [];
  for (const row of run) {
    if (row.kind === "agent_tool") {
      if (flags.hideToolCalls) continue;
      const parsed = parseToolBody(row.body);
      tools.push({ id: row.id, name: parsed.name, detail: parsed.detail });
    } else {
      // agent_commentary
      if (flags.hideCommentary) continue;
      const body = row.body.trim();
      if (body) proseParts.push(body);
    }
  }
  const commentary = proseParts.join("\n\n");
  if (!commentary && tools.length === 0) return null;
  return { turnId, commentary, tools, final };
}

// Walk an ordered message list and collapse consecutive same-turn agent
// activity rows into a single synthetic preamble message carrying a
// PreambleBlock. Ordinary messages pass through untouched and keep their order.
export function coalesceAgentActivity(messages: Message[], flags: AgentActivityFlags): Message[] {
  const out: Message[] = [];
  let i = 0;
  while (i < messages.length) {
    const current = messages[i];
    if (!isAgentActivity(current)) {
      out.push(current);
      i += 1;
      continue;
    }
    const key = turnKey(current);
    const run: Message[] = [];
    let j = i;
    while (j < messages.length && isAgentActivity(messages[j]) && turnKey(messages[j]) === key) {
      run.push(messages[j]);
      j += 1;
    }
    // The turn is final once anything follows the run; while the run is the
    // newest content in the channel the turn is still live.
    const final = j < messages.length;
    const block = buildBlock(key, run, final, flags);
    if (block) {
      // Synthesize one row from the first activity row in the run so author,
      // timestamp, channel/seq, and turn_id flow through grouping and the
      // virtualizer unchanged. The body is cleared; preamble_block drives
      // rendering.
      out.push({
        ...run[0],
        kind: "agent_commentary",
        body: "",
        attachments: undefined,
        quoted_message_id: undefined,
        preamble_block: block,
      });
    }
    i = j;
  }
  return out;
}
