export const prerender = false;
export const ssr = false;

import { api, readableAPIError } from "$lib/api";
import type { Project, Workspace } from "$lib/types";
import {
  listWorkspaceMembersPage,
  MEMBERS_PAGE_LIMIT,
  type WorkspaceMemberPage,
} from "$lib/workspace-members";

async function loadAllWorkspaceMembers(
  workspaceID: string,
): Promise<WorkspaceMemberPage["members"]> {
  const members: WorkspaceMemberPage["members"] = [];
  let cursor = "";
  let hasMore = true;
  while (hasMore) {
    const page = await listWorkspaceMembersPage({
      workspaceID,
      cursor: cursor || undefined,
      limit: MEMBERS_PAGE_LIMIT,
    });
    members.push(...page.members);
    hasMore = page.has_more;
    if (!hasMore) break;
    if (!page.next_cursor || page.next_cursor === cursor) {
      throw new Error("Could not continue loading workspace participants");
    }
    cursor = page.next_cursor;
  }
  return members;
}

export async function load({ params }: { params: { workspaceID: string } }) {
  let loadError = "";
  try {
    const workspaceData = await api<{ workspaces: Workspace[] }>("/api/workspaces");
    const workspace = workspaceData.workspaces.find(
      (item) => item.id === params.workspaceID || item.route_id === params.workspaceID,
    );
    if (!workspace) {
      return {
        workspaceID: params.workspaceID,
        workspace: undefined,
        projects: [] as Project[],
        members: [] as WorkspaceMemberPage["members"],
        loadError: "Workspace not found",
      };
    }
    const [projectData, members] = await Promise.all([
      api<{ projects: Project[] }>(`/api/workspaces/${workspace.id}/projects`),
      loadAllWorkspaceMembers(workspace.id),
    ]);
    return {
      workspaceID: workspace.id,
      workspace,
      projects: projectData.projects,
      members: members.filter((member) => member.role !== "guest"),
      loadError,
    };
  } catch (error) {
    loadError = readableAPIError(error, "Could not load projects");
    return {
      workspaceID: params.workspaceID,
      workspace: undefined,
      projects: [] as Project[],
      members: [] as WorkspaceMemberPage["members"],
      loadError,
    };
  }
}
