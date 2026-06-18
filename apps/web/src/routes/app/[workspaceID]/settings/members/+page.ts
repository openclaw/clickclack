export const prerender = false;
export const ssr = false;

import {
  listWorkspaceMembersPage,
  memberLoadErrorMessage,
  MEMBERS_PAGE_LIMIT,
  type WorkspaceMember,
  type WorkspaceMemberRoleCounts,
} from "../../../../../lib/workspace-members";
import type { Workspace } from "../../../../../lib/types";

export async function load({
  params,
  parent,
}: {
  params: { workspaceID: string };
  parent: () => Promise<{ workspace?: Workspace }>;
}) {
  const { workspace } = await parent();
  const workspaceID = workspace?.id ?? params.workspaceID;
  let members: WorkspaceMember[] = [];
  let nextCursor = "";
  let hasMore = false;
  let totalCount: number | undefined;
  let totalByRole: WorkspaceMemberRoleCounts | undefined;
  let loadError = "";
  try {
    const page = await listWorkspaceMembersPage({
      workspaceID,
      limit: MEMBERS_PAGE_LIMIT,
    });
    members = page.members;
    nextCursor = page.next_cursor ?? "";
    hasMore = page.has_more;
    totalCount = page.total_count;
    totalByRole = page.total_by_role;
  } catch (err) {
    loadError = memberLoadErrorMessage(err);
  }
  return {
    workspaceID,
    members,
    nextCursor,
    hasMore,
    totalCount,
    totalByRole,
    loadError,
  };
}
