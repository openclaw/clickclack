import { api, APIError } from "./api";
import type { User, Workspace } from "./types";

export type BotToken = {
  id: string;
  bot_user_id: string;
  workspace_id: string;
  owner_user_id?: string;
  name: string;
  scopes: string[];
  created_by?: string;
  created_at: string;
  last_used_at?: string;
  revoked_at?: string;
  token?: string;
};

export type BotWithTokens = {
  bot: User;
  tokens: BotToken[];
};

export type DeletedBot = {
  id: string;
  display_name: string;
  former_handle: string;
  deleted_at: string;
};

export type OwnedBotWorkspace = {
  id: string;
  route_id: string;
  name: string;
};

export type OwnedBotEntry = {
  bot: User;
  workspace: OwnedBotWorkspace;
  active_token_count: number;
};

export type BotScopeBundle = "bot:read" | "bot:write" | "bot:admin";

export const BOT_SCOPE_BUNDLES: { id: BotScopeBundle; label: string; hint: string }[] = [
  {
    id: "bot:read",
    label: "Read",
    hint: "View channels, messages, and threads. No write access.",
  },
  {
    id: "bot:write",
    label: "Read & write",
    hint: "Post and edit messages, send DMs, upload attachments, and publish command menus.",
  },
  {
    id: "bot:admin",
    label: "Admin",
    hint: "Read & write, publish command menus, and manage channels. Use sparingly.",
  },
];

export type CreateBotInput = {
  display_name: string;
  handle?: string;
  avatar_url?: string;
  owner_user_id?: string;
  token_name?: string;
  scopes?: string[];
  setup_nonce?: string;
  // false skips the initial token mint (setup-code installs mint the
  // token at claim time instead).
  initial_token?: boolean;
};

export type CreateBotResponse = {
  bot: User;
  // Omitted when the bot was created with initial_token: false.
  bot_token?: BotToken;
};

export type BotSetupCode = {
  id: string;
  bot_user_id: string;
  workspace_id: string;
  token_name: string;
  scopes: string[];
  defaults: BotSetupCodeDefaults;
  created_by?: string;
  created_at: string;
  expires_at: string;
  // One-time plaintext code (XXXX-XXXX-XXXX). Present only in the mint
  // response; the server stores just a hash.
  code?: string;
};

export type BotSetupCodeDefaults = {
  defaultTo?: string;
  allowFrom?: string[];
  agentActivity?: boolean;
};

export async function listWorkspaceBots(workspaceID: string): Promise<BotWithTokens[]> {
  const data = await api<{ bots: BotWithTokens[] }>(`/api/workspaces/${workspaceID}/bots`);
  return data.bots ?? [];
}

export async function createWorkspaceBot(
  workspaceID: string,
  input: CreateBotInput,
): Promise<CreateBotResponse> {
  return api<CreateBotResponse>(`/api/workspaces/${workspaceID}/bots`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function listWorkspaceBotTokens(
  workspaceID: string,
  botUserID: string,
): Promise<BotToken[]> {
  const data = await api<{ bot_tokens: BotToken[] }>(
    `/api/workspaces/${workspaceID}/bots/${botUserID}/tokens`,
  );
  return data.bot_tokens ?? [];
}

export async function createWorkspaceBotToken(
  workspaceID: string,
  botUserID: string,
  input: { name?: string; scopes?: string[]; setup_nonce?: string },
): Promise<BotToken> {
  const data = await api<{ bot_token: BotToken }>(
    `/api/workspaces/${workspaceID}/bots/${botUserID}/tokens`,
    {
      method: "POST",
      body: JSON.stringify(input),
    },
  );
  return data.bot_token;
}

export async function createWorkspaceBotSetupCode(
  workspaceID: string,
  botUserID: string,
  input: { name?: string; scopes?: string[]; defaults?: BotSetupCodeDefaults },
): Promise<BotSetupCode> {
  const data = await api<{ setup_code: BotSetupCode }>(
    `/api/workspaces/${workspaceID}/bots/${botUserID}/setup-codes`,
    {
      method: "POST",
      body: JSON.stringify(input),
    },
  );
  return data.setup_code;
}

export async function listBotTokens(botUserID: string): Promise<BotToken[]> {
  const data = await api<{ bot_tokens: BotToken[] }>(`/api/bots/${botUserID}/tokens`);
  return data.bot_tokens ?? [];
}

export async function createBotToken(
  botUserID: string,
  input: { name?: string; scopes?: string[]; setup_nonce?: string },
): Promise<BotToken> {
  const data = await api<{ bot_token: BotToken }>(`/api/bots/${botUserID}/tokens`, {
    method: "POST",
    body: JSON.stringify(input),
  });
  return data.bot_token;
}

export async function revokeBotToken(tokenID: string): Promise<BotToken> {
  const data = await api<{ bot_token: BotToken }>(`/api/bot-tokens/${tokenID}/revoke`, {
    method: "POST",
    body: JSON.stringify({}),
  });
  return data.bot_token;
}

export async function removeBotFromWorkspace(
  workspaceID: string,
  botUserID: string,
): Promise<void> {
  await api(`/api/workspaces/${workspaceID}/bots/${botUserID}/membership`, {
    method: "DELETE",
  });
}

export async function deleteBot(botUserID: string): Promise<DeletedBot> {
  const data = await api<{ deleted_bot: DeletedBot }>(`/api/bots/${botUserID}`, {
    method: "DELETE",
  });
  return data.deleted_bot;
}

export async function listMyBots(): Promise<OwnedBotEntry[]> {
  const data = await api<{ bots: OwnedBotEntry[] }>("/api/me/bots");
  return data.bots ?? [];
}

export function botLoadErrorMessage(err: unknown): string {
  if (err instanceof APIError) {
    if (err.status === 401) return "Sign in to manage bots.";
    if (err.status === 403) return "You don't have permission to manage bots in this workspace.";
    if (err.status === 404) return "That bot or workspace is no longer available.";
    if (err.status === 409) return "That handle is already taken. Try another.";
    if (err.status === 400) return err.message || "That request is invalid.";
  }
  return err instanceof Error ? err.message : "Something went wrong";
}

export function isServiceBot(bot: { owner_user_id?: string }): boolean {
  return !bot.owner_user_id;
}

export function activeTokens(tokens: BotToken[] | undefined): BotToken[] {
  if (!tokens) return [];
  return tokens.filter((t) => !t.revoked_at);
}

export function suggestHandleFrom(displayName: string): string {
  return displayName
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32);
}

export type OpenClawAccountMode = "single" | "named";

export function openClawWorkspaceIdentifier(workspace: Pick<Workspace, "id" | "slug">): string {
  return workspace.slug.trim() || workspace.id;
}

function jsonString(value: string): string {
  return JSON.stringify(value);
}

function envNameForHandle(handle: string): string {
  const suffix = handle
    .replace(/^@/, "")
    .toUpperCase()
    .replace(/[^A-Z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return suffix ? `CLICKCLACK_${suffix}_BOT_TOKEN` : "CLICKCLACK_BOT_TOKEN";
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

export function buildOpenClawConfigSnippet(opts: {
  workspace: string;
  botHandle: string;
  botUserID: string;
  mode: OpenClawAccountMode;
  baseURL?: string;
  defaultTo?: string;
  allowFrom?: string[];
  agentActivity?: boolean;
}): string {
  const base = (
    opts.baseURL || (typeof window !== "undefined" ? window.location.origin : "")
  ).replace(/\/$/, "");
  const handle = opts.botHandle.replace(/^@/, "");
  const envName = opts.mode === "single" ? "CLICKCLACK_BOT_TOKEN" : envNameForHandle(handle);
  const baseURL = base || "https://your-clickclack.example.com";
  const defaultTo = opts.defaultTo?.trim() || "channel:general";

  // Account-level lines shared by both single and named-account shapes.
  const accountLines = (indent: string): string => {
    const lines = [
      `workspace: ${jsonString(opts.workspace)},`,
      `botUserId: ${jsonString(opts.botUserID)},`,
      `defaultTo: ${jsonString(defaultTo)},`,
    ];
    if (opts.allowFrom && opts.allowFrom.length > 0 && !opts.allowFrom.includes("*")) {
      lines.push(`allowFrom: [${opts.allowFrom.map(jsonString).join(", ")}],`);
    }
    if (opts.agentActivity) {
      lines.push(`agentActivity: true,`);
    }
    return lines.map((line) => indent + line).join("\n");
  };

  if (opts.mode === "named") {
    return `{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${jsonString(baseURL)},
      defaultAccount: ${jsonString(handle)},
      accounts: {
        ${jsonString(handle)}: {
          token: { source: "env", provider: "default", id: ${jsonString(envName)} },
${accountLines("          ")}
        },
      },
    },
  },
}`;
  }

  return `{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${jsonString(baseURL)},
      token: { source: "env", provider: "default", id: ${jsonString(envName)} },
${accountLines("      ")}
    },
  },
}`;
}

export function buildOpenClawCodeSnippet(opts: {
  code: string;
  botHandle: string;
  mode: OpenClawAccountMode;
  baseURL?: string;
}): string {
  const base =
    (opts.baseURL || (typeof window !== "undefined" ? window.location.origin : "")).replace(
      /\/$/,
      "",
    ) || "https://your-clickclack.example.com";
  const handle = opts.botHandle.replace(/^@/, "");
  const accountArg = opts.mode === "named" ? ` --account ${shellQuote(handle)}` : "";
  return `openclaw channels add clickclack${accountArg} --code ${shellQuote(`${base}/#${opts.code}`)}`;
}

export function buildOpenClawShellSnippet(opts: {
  botHandle: string;
  token: string;
  mode: OpenClawAccountMode;
  workspace: string;
  baseURL?: string;
}): string {
  const base =
    (opts.baseURL || (typeof window !== "undefined" ? window.location.origin : "")).replace(
      /\/$/,
      "",
    ) || "https://your-clickclack.example.com";
  const handle = opts.botHandle.replace(/^@/, "");
  const accountLine = opts.mode === "named" ? ` \\\n  --account ${shellQuote(handle)}` : "";
  return `openclaw channels add clickclack${accountLine} \\
  --base-url ${shellQuote(base)} \\
  --token ${shellQuote(opts.token)} \\
  --workspace ${shellQuote(opts.workspace)}`;
}
