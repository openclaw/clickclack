export const prerender = false;
export const ssr = false;

import { api, APIError } from "../../../../../lib/api";
import type { User } from "../../../../../lib/types";

type Member = {
  workspace_id: string;
  user: User;
  role: string;
};

export async function load({ params }: { params: { workspaceID: string } }) {
  let members: Member[] = [];
  let loadError = "";
  try {
    const data = await api<{ members: Member[] }>(
      `/api/workspaces/${params.workspaceID}/moderation/members`,
    );
    members = data.members;
  } catch (err) {
    if (err instanceof APIError && (err.status === 401 || err.status === 403)) {
      loadError = "You don't have permission to view this workspace's members.";
    } else {
      loadError = err instanceof Error ? err.message : "Could not load members";
    }
  }
  return { members, loadError };
}
