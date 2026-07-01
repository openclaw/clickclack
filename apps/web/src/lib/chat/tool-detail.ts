// Tool-call presentation detail for coalesced preamble blocks.
//
// Each agent_tool row carries a tool name (the bridge's toolName/title) and an
// optional argument-derived detail string. To give the tool sub-items a
// readable label we derive a stable glyph + short human action from the name,
// mirroring the clickglass tool-line treatment so the two surfaces read alike.

export type ToolDetail = {
  // The raw tool name as stored (e.g. "exec", "read", "browserControl").
  name: string;
  // A small glyph hint for the tool category.
  glyph: string;
  // A short human action label (e.g. "Ran shell command", "Edited file").
  action: string;
  // A short "what/where" summary derived from the call (command, path, url).
  detail?: string;
};

const EXACT: Record<string, { glyph: string; action: string }> = {
  bash: { glyph: "\u{1F5A5}", action: "Ran shell command" },
  exec: { glyph: "\u{1F5A5}", action: "Ran shell command" },
  command: { glyph: "\u{1F5A5}", action: "Ran shell command" },
  bashsession: { glyph: "\u{1F5A5}", action: "Managed shell session" },
  sessions_send: { glyph: "\u{1F5A5}", action: "Managed shell session" },
  read: { glyph: "\u{1F4D6}", action: "Read file" },
  write: { glyph: "\u{1F4DD}", action: "Wrote file" },
  edit: { glyph: "\u270F", action: "Edited file" },
  apply_patch: { glyph: "\u{1F9E9}", action: "Applied patch" },
  applypatch: { glyph: "\u{1F9E9}", action: "Applied patch" },
  browsercontrol: { glyph: "\u{1F310}", action: "Controlled browser" },
  websearch: { glyph: "\u{1F50D}", action: "Searched the web" },
  webfetch: { glyph: "\u{1F517}", action: "Fetched a page" },
  image: { glyph: "\u{1F5BC}", action: "Analyzed image" },
  imagecreate: { glyph: "\u{1F3A8}", action: "Generated image" },
  scheduler: { glyph: "\u23F0", action: "Scheduled a job" },
  sendmessage: { glyph: "\u{1F4E4}", action: "Sent a message" },
  message: { glyph: "\u{1F4E4}", action: "Sent a message" },
  systemctl: { glyph: "\u2699", action: "Controlled the system" },
  process: { glyph: "\u2699", action: "Managed a process" },
  devicecontrol: { glyph: "\u{1F4F1}", action: "Controlled a device" },
  pdfparse: { glyph: "\u{1F4C4}", action: "Parsed a PDF" },
  statuscheck: { glyph: "\u{1F4CA}", action: "Checked status" },
};

type Rule = { test: (lower: string) => boolean; glyph: string; action: string };

const RULES: Rule[] = [
  { test: (l) => l.includes("search"), glyph: "\u{1F50D}", action: "Searched" },
  { test: (l) => l.includes("fetch"), glyph: "\u{1F517}", action: "Fetched" },
  { test: (l) => l.includes("browser"), glyph: "\u{1F310}", action: "Controlled browser" },
  { test: (l) => l.includes("patch"), glyph: "\u{1F9E9}", action: "Applied patch" },
  { test: (l) => l.includes("write"), glyph: "\u{1F4DD}", action: "Wrote file" },
  { test: (l) => l.includes("edit"), glyph: "\u270F", action: "Edited file" },
  { test: (l) => l.includes("read"), glyph: "\u{1F4D6}", action: "Read file" },
  { test: (l) => l.includes("session"), glyph: "\u{1F5A5}", action: "Managed shell session" },
  {
    test: (l) => l.includes("bash") || l.includes("shell") || l.includes("exec"),
    glyph: "\u{1F5A5}",
    action: "Ran command",
  },
  { test: (l) => l.includes("image"), glyph: "\u{1F5BC}", action: "Worked with image" },
  {
    test: (l) => l.includes("message") || l.includes("send"),
    glyph: "\u{1F4E4}",
    action: "Sent a message",
  },
  {
    test: (l) => l.includes("agent") || l.includes("subagent"),
    glyph: "\u{1F916}",
    action: "Ran a sub-agent",
  },
  {
    test: (l) => l.includes("schedul") || l.includes("cron"),
    glyph: "\u23F0",
    action: "Scheduled a job",
  },
  { test: (l) => l.includes("device"), glyph: "\u{1F4F1}", action: "Controlled a device" },
  {
    test: (l) => l.includes("file") || l.includes("dir"),
    glyph: "\u{1F4C1}",
    action: "Worked with files",
  },
];

export function toolDetail(rawName: string, detail?: string): ToolDetail {
  const name = String(rawName || "").trim();
  const lower = name.toLowerCase().replace(/[^a-z0-9_]/g, "");
  const trimmedDetail = String(detail || "").trim() || undefined;
  const exact = EXACT[lower];
  if (exact) return { name, glyph: exact.glyph, action: exact.action, detail: trimmedDetail };
  for (const rule of RULES) {
    if (rule.test(lower))
      return { name, glyph: rule.glyph, action: rule.action, detail: trimmedDetail };
  }
  // Unknown or empty name: a generic tool glyph. When the name is empty the
  // detail (the full body) carries the meaning.
  return { name: name || "tool", glyph: "\u{1F527}", action: "Used tool", detail: trimmedDetail };
}
