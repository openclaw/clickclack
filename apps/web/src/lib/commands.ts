import { api, apiURL, APIError } from "./api";
import type { SlashCommand, WorkspaceBotCommand } from "./types";

// Result of POST /api/hooks/slash/{channel_id}. `in_channel` responses are
// posted as bot messages server-side and arrive over realtime; anything else
// (Slack-style `ephemeral`) is only visible to the invoker.
export type SlashDispatchResult = {
  response_type: string;
  text: string;
};

export async function listBotCommands(workspaceID: string): Promise<WorkspaceBotCommand[]> {
  const data = await api<{ bot_commands: WorkspaceBotCommand[] }>(
    `/api/workspaces/${workspaceID}/bot-commands`,
  );
  return data.bot_commands ?? [];
}

export function normalizeCommandToken(raw: string): string {
  const trimmed = raw.trim().toLowerCase();
  if (!trimmed) return "";
  return trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
}

// Splits "/deploy prod --fast" into command "/deploy" and text "prod --fast".
// Returns null when the draft is not a slash-command shape.
export function splitSlashDraft(body: string): { command: string; text: string } | null {
  const match = /^(\/\S+)\s*([\s\S]*)$/.exec(body);
  if (!match) return null;
  return { command: normalizeCommandToken(match[1]), text: match[2].trim() };
}

// HTTP-registered commands win over bot-declared menu entries at dispatch
// time; bot-declared and unknown commands fall through to a plain message.
export function findRegisteredCommand(
  commands: SlashCommand[],
  commandToken: string,
): SlashCommand | undefined {
  return commands.find(
    (command) => !command.revoked_at && normalizeCommandToken(command.command) === commandToken,
  );
}

// The hook endpoint parses an application/x-www-form-urlencoded body
// (r.ParseForm), so this bypasses the JSON-only api() helper on purpose.
export async function dispatchSlashCommand(
  channelID: string,
  command: string,
  text: string,
): Promise<SlashDispatchResult> {
  const form = new URLSearchParams();
  form.set("command", command);
  form.set("text", text);
  const response = await fetch(apiURL(`/api/hooks/slash/${channelID}`), {
    method: "POST",
    credentials: "include",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/x-www-form-urlencoded",
      "X-ClickClack-CSRF": "1",
    },
    body: form.toString(),
  });
  const raw = await response.text();
  if (!response.ok) {
    throw new APIError(response.status, raw);
  }
  const data = raw ? (JSON.parse(raw) as Partial<SlashDispatchResult>) : {};
  return {
    response_type: data.response_type || "in_channel",
    text: (data.text || "").trim(),
  };
}
