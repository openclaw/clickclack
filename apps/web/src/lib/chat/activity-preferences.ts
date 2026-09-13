export type ActivityPreferences = {
  hideCommentary: boolean;
  hideToolCalls: boolean;
  userAlign: "left" | "right";
  otherAlign: "left" | "right";
};

const SHOW_AGENT_ACTIVITY_STORAGE_KEY = "clickclack:show-agent-activity:v1";
const HIDE_COMMENTARY_STORAGE_KEY = "clickclack:hide-commentary:v1";
const HIDE_TOOL_CALLS_STORAGE_KEY = "clickclack:hide-tool-calls:v1";
const USER_ALIGN_STORAGE_KEY = "clickclack:user-align:v1";
const OTHER_ALIGN_STORAGE_KEY = "clickclack:other-align:v1";

const storageKeys = {
  hideCommentary: HIDE_COMMENTARY_STORAGE_KEY,
  hideToolCalls: HIDE_TOOL_CALLS_STORAGE_KEY,
  userAlign: USER_ALIGN_STORAGE_KEY,
  otherAlign: OTHER_ALIGN_STORAGE_KEY,
};

function loadActivityVisibility(key: string, legacyHidden: boolean): boolean {
  const stored = window.localStorage.getItem(key);
  // An explicit current choice takes precedence over the old combined setting.
  return stored === "0" ? false : stored === "1" || legacyHidden;
}

export function loadActivityPreferences(): ActivityPreferences {
  const preferences: ActivityPreferences = {
    hideCommentary: false,
    hideToolCalls: false,
    userAlign: "left",
    otherAlign: "left",
  };
  try {
    const legacyHidden = window.localStorage.getItem(SHOW_AGENT_ACTIVITY_STORAGE_KEY) === "0";
    preferences.hideCommentary = loadActivityVisibility(HIDE_COMMENTARY_STORAGE_KEY, legacyHidden);
    preferences.hideToolCalls = loadActivityVisibility(HIDE_TOOL_CALLS_STORAGE_KEY, legacyHidden);
    preferences.userAlign =
      window.localStorage.getItem(USER_ALIGN_STORAGE_KEY) === "right" ? "right" : "left";
    preferences.otherAlign =
      window.localStorage.getItem(OTHER_ALIGN_STORAGE_KEY) === "right" ? "right" : "left";
  } catch {
    preferences.hideCommentary = false;
    preferences.hideToolCalls = false;
    preferences.userAlign = "left";
    preferences.otherAlign = "left";
  }
  return preferences;
}

export function storeActivityPreference<Key extends keyof ActivityPreferences>(
  key: Key,
  value: ActivityPreferences[Key],
) {
  try {
    window.localStorage.setItem(
      storageKeys[key],
      typeof value === "boolean" ? (value ? "1" : "0") : value,
    );
  } catch {
    // The in-memory choice still applies when storage is unavailable.
  }
}

export function applyMessageAlignments(
  userAlign: ActivityPreferences["userAlign"],
  otherAlign: ActivityPreferences["otherAlign"],
) {
  try {
    document.documentElement.setAttribute("data-user-align", userAlign);
    document.documentElement.setAttribute("data-other-align", otherAlign);
  } catch {
    // SSR has no document; preferences are applied again on mount.
  }
}
