import { api, APIError } from "./api";
import type { User } from "./types";

export type WorkspaceMemberRole = "owner" | "moderator" | "member" | "bot" | "guest";

export type WorkspaceMember = {
  workspace_id: string;
  user: User;
  role: WorkspaceMemberRole;
  joined_at: string;
};

export type WorkspaceMemberRoleCounts = Record<WorkspaceMemberRole, number>;

export type WorkspaceMemberPage = {
  members: WorkspaceMember[];
  next_cursor?: string;
  has_more: boolean;
  total_count?: number;
  total_by_role?: WorkspaceMemberRoleCounts;
};

export type ListWorkspaceMembersParams = {
  workspaceID: string;
  cursor?: string;
  query?: string;
  role?: WorkspaceMemberRole | "";
  limit?: number;
};

export const MEMBERS_PAGE_LIMIT = 100;

export async function listWorkspaceMembersPage(
  params: ListWorkspaceMembersParams,
): Promise<WorkspaceMemberPage> {
  const search = new URLSearchParams();
  if (params.limit) search.set("limit", String(params.limit));
  if (params.cursor) search.set("cursor", params.cursor);
  const trimmed = params.query?.trim();
  if (trimmed) search.set("q", trimmed);
  if (params.role) search.set("role", params.role);
  const qs = search.toString();
  const path = `/api/workspaces/${params.workspaceID}/members${qs ? `?${qs}` : ""}`;
  return api<WorkspaceMemberPage>(path);
}

export function memberLoadErrorMessage(err: unknown): string {
  if (err instanceof APIError) {
    if (err.status === 401 || err.status === 403) {
      return "You don't have permission to view this workspace's members.";
    }
    if (err.status === 400) {
      return "That search isn't valid. Try a different query or filter.";
    }
  }
  return err instanceof Error ? err.message : "Could not load members";
}
