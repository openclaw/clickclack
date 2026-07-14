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

  const [
    installationsResult,
    commandsResult,
    subscriptionsResult,
    accountsResult,
    botsResult,
    channelsResult,
    eventTypesResult,
    meResult,
  ] = await Promise.allSettled([
    listAppInstallations(workspaceID),
    listSlashCommands(workspaceID),
    listEventSubscriptions(workspaceID),
    listConnectedAccounts(workspaceID),
    listWorkspaceBots(workspaceID),
    api<{ channels: Channel[] }>(`/api/workspaces/${workspaceID}/channels`),
    listEventTypes(),
    api<{ user: User }>("/api/me"),
  ]);

  if (installationsResult.status === "fulfilled") installations = installationsResult.value;
  if (commandsResult.status === "fulfilled") commands = commandsResult.value;
  if (subscriptionsResult.status === "fulfilled") subscriptions = subscriptionsResult.value;
  if (accountsResult.status === "fulfilled") connectedAccounts = accountsResult.value;
  if (botsResult.status === "fulfilled") bots = botsResult.value;
  if (channelsResult.status === "fulfilled") channels = channelsResult.value.channels ?? [];
  if (eventTypesResult.status === "fulfilled") eventTypes = eventTypesResult.value;
  if (meResult.status === "fulfilled") me = meResult.value.user;

  const failureMessages = [
    installationsResult,
    commandsResult,
    subscriptionsResult,
    accountsResult,
    botsResult,
    channelsResult,
    eventTypesResult,
    meResult,
  ]
    .filter((result): result is PromiseRejectedResult => result.status === "rejected")
    .map((result) => integrationsLoadErrorMessage(result.reason));
  if (failureMessages.length > 0) {
    loadError = `Some integration data could not be loaded. ${[...new Set(failureMessages)].join(" ")}`;
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
    loaded: {
      installations: installationsResult.status === "fulfilled",
      commands: commandsResult.status === "fulfilled",
      subscriptions: subscriptionsResult.status === "fulfilled",
      connectedAccounts: accountsResult.status === "fulfilled",
      bots: botsResult.status === "fulfilled",
      channels: channelsResult.status === "fulfilled",
      eventTypes: eventTypesResult.status === "fulfilled",
      me: meResult.status === "fulfilled",
    },
    loadError,
  };
}
