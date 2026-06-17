// Settings is split in two:
//
// 1. Account settings (Profile, Notifications) render inside a modal
//    overlay (SettingsModal.svelte). These belong to the signed-in user
//    and have no URL; opening one just toggles modal state.
//
// 2. Workspace settings (members, bots, integrations, etc.) live at the
//    real route /app/{workspaceID}/settings and have their own shell.

export type AccountSettingsSectionId = "profile" | "notifications";

export type AccountSettingsSection = {
  id: AccountSettingsSectionId;
  label: string;
};

// Account modal sections (rendered in order in the rail).
export const ACCOUNT_SETTINGS_SECTIONS: AccountSettingsSection[] = [
  { id: "profile", label: "Profile" },
  { id: "notifications", label: "Notifications" },
];

export const DEFAULT_ACCOUNT_SETTINGS_SECTION: AccountSettingsSectionId = "profile";

export function workspaceSettingsPath(workspaceID: string): string {
  return `/app/${workspaceID}/settings`;
}
