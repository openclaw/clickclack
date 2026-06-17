export const prerender = false;
export const ssr = false;

import { redirect } from "@sveltejs/kit";
import {
  DEFAULT_WORKSPACE_SETTINGS_SECTION,
  workspaceSettingsPath,
} from "../../../../lib/settings";

export function load({ params }: { params: { workspaceID: string } }) {
  throw redirect(
    307,
    workspaceSettingsPath(params.workspaceID, DEFAULT_WORKSPACE_SETTINGS_SECTION),
  );
}
