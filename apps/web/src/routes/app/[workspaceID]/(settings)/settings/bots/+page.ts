export const prerender = false;
export const ssr = false;

import { APIError } from "#lib/api";
import { requestCurrentUser } from "#lib/appearance";
import {
  listWorkspaceBots,
  botLoadErrorMessage,
  openClawWorkspaceIdentifier,
  type BotWithTokens,
} from "#lib/bots";
import type { User, Workspace } from "#lib/types";

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
  let bots: BotWithTokens[] = [];
  let me: User | null = null;
  let loadError = "";
  try {
    const [botsResult, meResult] = await Promise.all([
      listWorkspaceBots(workspaceID),
      requestCurrentUser(),
    ]);
    bots = botsResult;
    me = meResult.user;
  } catch (err) {
    if (err instanceof APIError && (err.status === 401 || err.status === 403)) {
      loadError = botLoadErrorMessage(err);
    } else {
      loadError = botLoadErrorMessage(err);
    }
  }
  return {
    workspaceID,
    workspaceIdentifier,
    workspace,
    bots,
    me,
    loadError,
  };
}
