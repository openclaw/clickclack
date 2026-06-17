export const prerender = false;
export const ssr = false;

import { api, APIError } from "../../../../lib/api";
import type { Workspace } from "../../../../lib/types";

export async function load({ params }: { params: { workspaceID: string } }) {
  const workspaceID = params.workspaceID;
  let workspaces: Workspace[] = [];
  let loadError = "";
  try {
    const data = await api<{ workspaces: Workspace[] }>("/api/workspaces");
    workspaces = data.workspaces;
  } catch (err) {
    loadError =
      err instanceof APIError
        ? err.message
        : err instanceof Error
          ? err.message
          : "Could not load workspace";
  }
  const workspace = workspaces.find((w) => w.id === workspaceID || w.route_id === workspaceID);
  return { workspaceID, workspace, workspaces, loadError };
}
