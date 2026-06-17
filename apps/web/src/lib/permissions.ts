// Workspace role helpers. UI gates here are advisory — the backend is
// always the source of truth. Use these to hide/disable UI; never to
// authorize an action.

import type { Workspace } from "./types";

export type WorkspaceRole = NonNullable<Workspace["role"]>;

const MANAGER_ROLES = new Set<WorkspaceRole>(["owner", "moderator"]);

export function isWorkspaceManager(role: WorkspaceRole | undefined | null): boolean {
  if (!role) return false;
  return MANAGER_ROLES.has(role);
}

export function currentRole(
  workspaces: readonly Workspace[],
  workspaceID: string | undefined | null,
): WorkspaceRole | undefined {
  if (!workspaceID) return undefined;
  const match = workspaces.find(
    (workspace) => workspace.id === workspaceID || workspace.route_id === workspaceID,
  );
  return match?.role;
}
