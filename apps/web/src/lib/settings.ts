// Settings is split in two:
//
// 1. Account settings (Profile, Notifications) render inside a modal
//    overlay (SettingsModal.svelte). These belong to the signed-in user
//    and have no URL; opening one just toggles modal state.
//
// 2. Workspace settings (members, bots, integrations, etc.) live at the
//    real route /app/{workspaceID}/settings and have their own shell.

export type AccountSettingsSectionId = "profile" | "notifications" | "bots";

export type AccountSettingsSection = {
  id: AccountSettingsSectionId;
  label: string;
};

// Account modal sections (rendered in order in the rail).
export const ACCOUNT_SETTINGS_SECTIONS: AccountSettingsSection[] = [
  { id: "profile", label: "Profile" },
  { id: "notifications", label: "Notifications" },
  { id: "bots", label: "My bots" },
];

export const DEFAULT_ACCOUNT_SETTINGS_SECTION: AccountSettingsSectionId = "profile";

// Workspace settings sections. The `slug` is the URL segment under
// /app/{workspaceID}/settings/. `group` controls which rail heading
// the item lives under. `managersOnly` hides the item from the rail
// when the current user is not an owner or moderator (the page itself
// must still re-check via the backend; this is just a UI gate).
export type WorkspaceSettingsSectionId = "overview" | "members" | "bots";

export type WorkspaceSettingsGroupId = "workspace" | "people" | "automation";

export type WorkspaceSettingsSection = {
  id: WorkspaceSettingsSectionId;
  slug: string;
  label: string;
  group: WorkspaceSettingsGroupId;
  // Inline SVG path data strings for the rail icon (24×24 stroke icons).
  icon: string[];
  managersOnly?: boolean;
};

export const WORKSPACE_SETTINGS_GROUPS: { id: WorkspaceSettingsGroupId; label: string }[] = [
  { id: "workspace", label: "Workspace" },
  { id: "people", label: "People" },
  { id: "automation", label: "Automation" },
];

export const WORKSPACE_SETTINGS_SECTIONS: WorkspaceSettingsSection[] = [
  {
    id: "overview",
    slug: "overview",
    label: "Overview",
    group: "workspace",
    icon: [
      "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z",
      "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z",
    ],
  },
  {
    id: "members",
    slug: "members",
    label: "Members",
    group: "people",
    icon: [
      "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2",
      "M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z",
      "M22 21v-2a4 4 0 0 0-3-3.87",
      "M16 3.13a4 4 0 0 1 0 7.75",
    ],
  },
  {
    id: "bots",
    slug: "bots",
    label: "Bots & agents",
    group: "automation",
    icon: [
      "M12 8V4H8",
      "M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z",
      "M2 14h2",
      "M20 14h2",
      "M15 13v2",
      "M9 13v2",
    ],
  },
];

export const DEFAULT_WORKSPACE_SETTINGS_SECTION: WorkspaceSettingsSectionId = "overview";

export function workspaceSettingsPath(workspaceID: string, slug?: string): string {
  const base = `/app/${workspaceID}/settings`;
  return slug ? `${base}/${slug}` : base;
}
