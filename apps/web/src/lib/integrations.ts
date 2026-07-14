// Typed wrapper over the workspace integrations control plane:
// app installations, slash commands, event subscriptions (+ delivery
// attempts), connected accounts, and the durable event-type vocabulary.
// Mirrors the lib/bots.ts pattern: one request fn per endpoint, thin
// helpers, backend remains the authorization source of truth.
import { api, APIError } from "./api";

export type AppInstallation = {
  id: string;
  workspace_id: string;
  app_slug: string;
  display_name: string;
  bot_user_id: string;
  config: Record<string, unknown> | null;
  created_by?: string;
  created_at: string;
  revoked_at?: string | null;
};

export type SlashCommand = {
  id: string;
  workspace_id: string;
  app_installation_id?: string;
  command: string;
  description: string;
  callback_url: string;
  // Populated only on create and rotate-secret responses.
  signing_secret?: string;
  bot_user_id: string;
  created_by?: string;
  created_at: string;
  revoked_at?: string | null;
};

export type EventSubscription = {
  id: string;
  workspace_id: string;
  app_installation_id?: string;
  event_types: string[];
  callback_url: string;
  // Populated only on create and rotate-secret responses.
  signing_secret?: string;
  created_by?: string;
  created_at: string;
  revoked_at?: string | null;
};

export type EventDeliveryAttempt = {
  id: string;
  subscription_id: string;
  event_id: string;
  workspace_id: string;
  event_type: string;
  attempt: number;
  request_json?: string;
  response_status: number;
  response_body?: string;
  error?: string;
  created_at: string;
  completed_at: string;
};

export type ConnectedAccount = {
  id: string;
  workspace_id: string;
  user_id: string;
  provider: string;
  provider_account_id: string;
  display_name: string;
  scopes: string[];
  metadata: Record<string, unknown> | null;
  created_at: string;
  revoked_at?: string | null;
};

export type RevokeInstallationOptions = {
  revoke_slash_commands?: boolean;
  revoke_event_subscriptions?: boolean;
  revoke_bot_tokens?: boolean;
};

export type RevokeInstallationResult = {
  installation: AppInstallation;
  revoked: {
    slash_commands: number;
    event_subscriptions: number;
    bot_tokens: number;
  };
};

export type DeliveriesPage = {
  deliveries: EventDeliveryAttempt[];
  next_cursor: string | null;
};

// --- App installations ---

export async function listAppInstallations(workspaceID: string): Promise<AppInstallation[]> {
  const data = await api<{ app_installations: AppInstallation[] }>(
    `/api/workspaces/${workspaceID}/app-installations`,
  );
  return data.app_installations ?? [];
}

export async function createAppInstallation(
  workspaceID: string,
  input: {
    app_slug: string;
    display_name: string;
    bot_user_id: string;
    config?: Record<string, unknown>;
  },
): Promise<AppInstallation> {
  const data = await api<{ app_installation: AppInstallation }>(
    `/api/workspaces/${workspaceID}/app-installations`,
    { method: "POST", body: JSON.stringify(input) },
  );
  return data.app_installation;
}

export async function revokeAppInstallation(
  installationID: string,
  options: RevokeInstallationOptions = {},
): Promise<RevokeInstallationResult> {
  return api<RevokeInstallationResult>(`/api/app-installations/${installationID}/revoke`, {
    method: "POST",
    body: JSON.stringify(options),
  });
}

// --- Slash commands ---

export async function listSlashCommands(workspaceID: string): Promise<SlashCommand[]> {
  const data = await api<{ slash_commands: SlashCommand[] }>(
    `/api/workspaces/${workspaceID}/slash-commands`,
  );
  return data.slash_commands ?? [];
}

export async function createSlashCommand(
  workspaceID: string,
  input: {
    app_installation_id?: string;
    command: string;
    description?: string;
    callback_url: string;
    bot_user_id: string;
  },
): Promise<SlashCommand> {
  const data = await api<{ slash_command: SlashCommand }>(
    `/api/workspaces/${workspaceID}/slash-commands`,
    { method: "POST", body: JSON.stringify(input) },
  );
  return data.slash_command;
}

export async function revokeSlashCommand(commandID: string): Promise<SlashCommand> {
  const data = await api<{ slash_command: SlashCommand }>(
    `/api/slash-commands/${commandID}/revoke`,
    { method: "POST", body: JSON.stringify({}) },
  );
  return data.slash_command;
}

export async function rotateSlashCommandSecret(commandID: string): Promise<SlashCommand> {
  const data = await api<{ slash_command: SlashCommand }>(
    `/api/slash-commands/${commandID}/rotate-secret`,
    { method: "POST", body: JSON.stringify({}) },
  );
  return data.slash_command;
}

// --- Event subscriptions ---

export async function listEventSubscriptions(workspaceID: string): Promise<EventSubscription[]> {
  const data = await api<{ event_subscriptions: EventSubscription[] }>(
    `/api/workspaces/${workspaceID}/event-subscriptions`,
  );
  return data.event_subscriptions ?? [];
}

export async function createEventSubscription(
  workspaceID: string,
  input: {
    app_installation_id?: string;
    event_types: string[];
    callback_url: string;
  },
): Promise<EventSubscription> {
  const data = await api<{ event_subscription: EventSubscription }>(
    `/api/workspaces/${workspaceID}/event-subscriptions`,
    { method: "POST", body: JSON.stringify(input) },
  );
  return data.event_subscription;
}

export async function revokeEventSubscription(subscriptionID: string): Promise<EventSubscription> {
  const data = await api<{ event_subscription: EventSubscription }>(
    `/api/event-subscriptions/${subscriptionID}/revoke`,
    { method: "POST", body: JSON.stringify({}) },
  );
  return data.event_subscription;
}

export async function rotateEventSubscriptionSecret(
  subscriptionID: string,
): Promise<EventSubscription> {
  const data = await api<{ event_subscription: EventSubscription }>(
    `/api/event-subscriptions/${subscriptionID}/rotate-secret`,
    { method: "POST", body: JSON.stringify({}) },
  );
  return data.event_subscription;
}

export async function listEventDeliveries(
  subscriptionID: string,
  opts: { limit?: number; before?: string } = {},
): Promise<DeliveriesPage> {
  const params = new URLSearchParams();
  if (opts.limit) params.set("limit", String(opts.limit));
  if (opts.before) params.set("before", opts.before);
  const suffix = params.toString();
  const data = await api<{
    deliveries: EventDeliveryAttempt[];
    next_cursor: string | null;
  }>(`/api/event-subscriptions/${subscriptionID}/deliveries${suffix ? `?${suffix}` : ""}`);
  return { deliveries: data.deliveries ?? [], next_cursor: data.next_cursor ?? null };
}

// --- Connected accounts ---

export async function listConnectedAccounts(workspaceID: string): Promise<ConnectedAccount[]> {
  const data = await api<{ connected_accounts: ConnectedAccount[] }>(
    `/api/workspaces/${workspaceID}/connected-accounts`,
  );
  return data.connected_accounts ?? [];
}

export async function revokeConnectedAccount(accountID: string): Promise<ConnectedAccount> {
  const data = await api<{ connected_account: ConnectedAccount }>(
    `/api/connected-accounts/${accountID}/revoke`,
    { method: "POST", body: JSON.stringify({}) },
  );
  return data.connected_account;
}

// --- Event types ---

export async function listEventTypes(): Promise<string[]> {
  const data = await api<{ event_types: string[] }>("/api/event-types");
  return data.event_types ?? [];
}

// --- Helpers ---

export function integrationsLoadErrorMessage(err: unknown): string {
  if (err instanceof APIError) {
    if (err.status === 401) return "Sign in to manage integrations.";
    if (err.status === 403)
      return "You don't have permission to manage integrations in this workspace.";
    if (err.status === 404) return "That integration is no longer available.";
    if (err.status === 400) return err.message || "That request is invalid.";
  }
  return err instanceof Error ? err.message : "Something went wrong";
}

export function isRevoked(entry: { revoked_at?: string | null }): boolean {
  return !!entry.revoked_at;
}

export function activeOnly<T extends { revoked_at?: string | null }>(entries: T[]): T[] {
  return entries.filter((entry) => !isRevoked(entry));
}

export function attachedTo<T extends { app_installation_id?: string }>(
  entries: T[],
  installationID: string,
): T[] {
  return entries.filter((entry) => entry.app_installation_id === installationID);
}

export function unattached<T extends { app_installation_id?: string }>(entries: T[]): T[] {
  return entries.filter((entry) => !entry.app_installation_id);
}
