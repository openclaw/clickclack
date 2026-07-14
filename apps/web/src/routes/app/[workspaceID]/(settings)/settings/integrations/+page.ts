export const prerender = false;
export const ssr = false;

import { api } from "$lib/api";
import {
  integrationsLoadErrorMessage,
  listAppInstallations,
  listConnectedAccounts,
  listEventSubscriptions,
  listEventTypes,
  listSlashCommands,
  type AppInstallation,
  type ConnectedAccount,
  type EventSubscription,
  type SlashCommand,
} from "$lib/integrations";
import { listWorkspaceBots, openClawWorkspaceIdentifier, type BotWithTokens } from "$lib/bots";
import type { Channel, User, Workspace } from "$lib/types";

export async function load({
  params,
  parent,
}: {
  params: { workspaceID: string };
  parent: () => Promise<{ workspace?: Workspace }>;
}) {
  const { workspace } = await parent();
  const workspaceID = workspace?.id ?? params.workspaceID;
  const workspaceIdentifier = workspace
    ? openClawWorkspaceIdentifier(workspace)
    : params.workspaceID;

  let installations: AppInstallation[] = [];
  let commands: SlashCommand[] = [];
  let subscriptions: EventSubscription[] = [];
  let connectedAccounts: ConnectedAccount[] = [];
  let bots: BotWithTokens[] = [];
  let channels: Channel[] = [];
  let eventTypes: string[] = [];
  let me: User | null = null;
  let loadError = "";

  try {
    const [
      installationsResult,
      commandsResult,
      subscriptionsResult,
      accountsResult,
      botsResult,
      channelsResult,
      eventTypesResult,
      meResult,
    ] = await Promise.all([
      listAppInstallations(workspaceID),
      listSlashCommands(workspaceID),
      listEventSubscriptions(workspaceID),
      listConnectedAccounts(workspaceID),
      listWorkspaceBots(workspaceID),
      api<{ channels: Channel[] }>(`/api/workspaces/${workspaceID}/channels`),
      listEventTypes(),
      api<{ user: User }>("/api/me"),
    ]);
    installations = installationsResult;
    commands = commandsResult;
    subscriptions = subscriptionsResult;
    connectedAccounts = accountsResult;
    bots = botsResult;
    channels = channelsResult.channels ?? [];
    eventTypes = eventTypesResult;
    me = meResult.user;
  } catch (err) {
    loadError = integrationsLoadErrorMessage(err);
  }

  return {
    workspaceID,
    workspaceIdentifier,
    workspace,
    installations,
    commands,
    subscriptions,
    connectedAccounts,
    bots,
    channels,
    eventTypes,
    me,
    loadError,
  };
}
