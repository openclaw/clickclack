// Appearance preferences: color mode, board theme, message layout, and density.
//
// Preferences roam with the account. localStorage remains the pre-paint cache,
// and data attributes on <html> drive the active styles. The inline app.html
// script reads these storage keys before hydration; keep them in sync.

import { api } from "./api";
import { getEmbedHostThemeMode } from "./embed-theme";
import type { AppearancePreferences, AppearancePreferencesPatch, User } from "./types";

export type ColorMode = "light" | "dark" | "system";
export type BoardTheme = "agora" | "signal" | "ember" | "moss" | "iris";
export type MessageLayout = "standard" | "outlined";
export type Density = "comfortable" | "compact";

export const COLOR_MODE_STORAGE_KEY = "clickclack:color-mode:v1";
export const BOARD_THEME_STORAGE_KEY = "clickclack:board-theme:v1";
export const MESSAGE_LAYOUT_STORAGE_KEY = "clickclack:message-layout:v1";
export const DENSITY_STORAGE_KEY = "clickclack:density:v1";
const APPEARANCE_CACHE_USER_STORAGE_KEY = "clickclack:appearance-user:v1";

export const DEFAULT_COLOR_MODE: ColorMode = "system";
export const DEFAULT_BOARD_THEME: BoardTheme = "agora";
export const DEFAULT_MESSAGE_LAYOUT: MessageLayout = "standard";
export const DEFAULT_DENSITY: Density = "comfortable";

export const COLOR_MODES: { id: ColorMode; label: string }[] = [
  { id: "light", label: "Light" },
  { id: "dark", label: "Dark" },
  { id: "system", label: "System" },
];

export const BOARD_THEMES: { id: BoardTheme; label: string; blurb: string }[] = [
  { id: "agora", label: "Marketscape", blurb: "Deep-space glass, gold deckplates, cyan telemetry" },
  { id: "signal", label: "Galactic", blurb: "Deep-space indigo, neon telemetry" },
  { id: "ember", label: "Ember", blurb: "Warm paper, ember coral" },
  { id: "moss", label: "Moss", blurb: "Sage plate, verdant green" },
  { id: "iris", label: "Iris", blurb: "Violet plate, twilight iris" },
];

export const MESSAGE_LAYOUTS: { id: MessageLayout; label: string; blurb: string }[] = [
  { id: "standard", label: "Standard", blurb: "Compact messages with activity attached" },
  {
    id: "outlined",
    label: "Outlined chains",
    blurb: "Every message outlined; agent activity caps its answer",
  },
];

export const DENSITIES: { id: Density; label: string; blurb: string }[] = [
  {
    id: "comfortable",
    label: "Comfortable",
    blurb: "Roomy rows and full-size avatars",
  },
  {
    id: "compact",
    label: "Compact",
    blurb: "Tighter rows fit more messages on screen",
  },
];

let appearanceSyncUserID = "";
let appearanceWriteQueue = Promise.resolve();
let appearanceRevision = 0;
let appearanceSettledRevision = 0;
let appearanceAccountGeneration = 0;
const migratedAppearanceUsers = new Set<string>();

type AppearanceRequestState = {
  userID: string;
  revision: number;
  settledRevision: number;
  accountGeneration: number;
};

function isColorMode(value: string | null): value is ColorMode {
  return value === "light" || value === "dark" || value === "system";
}

function isBoardTheme(value: string | null): value is BoardTheme {
  return BOARD_THEMES.some((board) => board.id === value);
}

function isMessageLayout(value: string | null): value is MessageLayout {
  return MESSAGE_LAYOUTS.some((layout) => layout.id === value);
}

function isDensity(value: string | null): value is Density {
  return DENSITIES.some((density) => density.id === value);
}

export function loadColorMode(): ColorMode {
  try {
    const stored = window.localStorage.getItem(COLOR_MODE_STORAGE_KEY);
    return isColorMode(stored) ? stored : DEFAULT_COLOR_MODE;
  } catch {
    return DEFAULT_COLOR_MODE;
  }
}

export function loadBoardTheme(): BoardTheme {
  try {
    const stored = window.localStorage.getItem(BOARD_THEME_STORAGE_KEY);
    return isBoardTheme(stored) ? stored : DEFAULT_BOARD_THEME;
  } catch {
    return DEFAULT_BOARD_THEME;
  }
}

export function loadMessageLayout(): MessageLayout {
  try {
    const stored = window.localStorage.getItem(MESSAGE_LAYOUT_STORAGE_KEY);
    return isMessageLayout(stored) ? stored : DEFAULT_MESSAGE_LAYOUT;
  } catch {
    return DEFAULT_MESSAGE_LAYOUT;
  }
}

export function loadDensity(): Density {
  try {
    const stored = window.localStorage.getItem(DENSITY_STORAGE_KEY);
    return isDensity(stored) ? stored : DEFAULT_DENSITY;
  } catch {
    return DEFAULT_DENSITY;
  }
}

export function applyColorMode(mode: ColorMode) {
  try {
    // Account refreshes must not replace the host-selected embed appearance;
    // the override is document-local and never enters account preferences.
    const resolvedMode = getEmbedHostThemeMode() ?? mode;
    if (resolvedMode === "system") document.documentElement.removeAttribute("data-color-mode");
    else document.documentElement.setAttribute("data-color-mode", resolvedMode);
  } catch {
    // Non-DOM context (SSR/tests); the stored pref still applies on mount.
  }
}

export function applyBoardTheme(board: BoardTheme) {
  try {
    if (board === DEFAULT_BOARD_THEME) document.documentElement.removeAttribute("data-board");
    else document.documentElement.setAttribute("data-board", board);
  } catch {
    // Non-DOM context (SSR/tests); the stored pref still applies on mount.
  }
}

export function applyMessageLayout(layout: MessageLayout) {
  try {
    if (layout === DEFAULT_MESSAGE_LAYOUT) {
      document.documentElement.removeAttribute("data-message-layout");
    } else {
      document.documentElement.setAttribute("data-message-layout", layout);
    }
  } catch {
    // Non-DOM context (SSR/tests); the stored pref still applies on mount.
  }
}

export function applyDensity(density: Density) {
  try {
    if (density === DEFAULT_DENSITY) {
      document.documentElement.removeAttribute("data-density");
    } else {
      document.documentElement.setAttribute("data-density", density);
    }
  } catch {
    // Non-DOM context (SSR/tests); the stored pref still applies on mount.
  }
}

function setLocalColorMode(mode: ColorMode) {
  applyColorMode(mode);
  try {
    if (mode === DEFAULT_COLOR_MODE) window.localStorage.removeItem(COLOR_MODE_STORAGE_KEY);
    else window.localStorage.setItem(COLOR_MODE_STORAGE_KEY, mode);
  } catch {
    // Ignore unavailable storage; the in-memory pref still applies this session.
  }
}

function setLocalBoardTheme(board: BoardTheme) {
  applyBoardTheme(board);
  try {
    if (board === DEFAULT_BOARD_THEME) window.localStorage.removeItem(BOARD_THEME_STORAGE_KEY);
    else window.localStorage.setItem(BOARD_THEME_STORAGE_KEY, board);
  } catch {
    // Ignore unavailable storage; the in-memory pref still applies this session.
  }
}

function setLocalMessageLayout(layout: MessageLayout) {
  applyMessageLayout(layout);
  try {
    if (layout === DEFAULT_MESSAGE_LAYOUT) {
      window.localStorage.removeItem(MESSAGE_LAYOUT_STORAGE_KEY);
    } else {
      window.localStorage.setItem(MESSAGE_LAYOUT_STORAGE_KEY, layout);
    }
  } catch {
    // Ignore unavailable storage; the in-memory pref still applies this session.
  }
}

function setLocalDensity(density: Density) {
  applyDensity(density);
  try {
    if (density === DEFAULT_DENSITY) {
      window.localStorage.removeItem(DENSITY_STORAGE_KEY);
    } else {
      window.localStorage.setItem(DENSITY_STORAGE_KEY, density);
    }
  } catch {
    // Ignore unavailable storage; the in-memory pref still applies this session.
  }
}

export function setColorMode(mode: ColorMode) {
  setLocalColorMode(mode);
  rememberAppearanceCacheUser();
  queueAppearancePatch({ color_mode: mode === DEFAULT_COLOR_MODE ? "" : mode });
}

export function setBoardTheme(board: BoardTheme) {
  setLocalBoardTheme(board);
  rememberAppearanceCacheUser();
  queueAppearancePatch({ board_theme: board === DEFAULT_BOARD_THEME ? "" : board });
}

export function setMessageLayout(layout: MessageLayout) {
  setLocalMessageLayout(layout);
  rememberAppearanceCacheUser();
  queueAppearancePatch({
    message_layout: layout === DEFAULT_MESSAGE_LAYOUT ? "" : layout,
  });
}

export function setDensity(density: Density) {
  setLocalDensity(density);
  rememberAppearanceCacheUser();
  queueAppearancePatch({ density: density === DEFAULT_DENSITY ? "" : density });
}

export function serializeAppearancePreferences(): AppearancePreferences {
  const colorMode = loadColorMode();
  const boardTheme = loadBoardTheme();
  const messageLayout = loadMessageLayout();
  const density = loadDensity();
  return {
    color_mode: colorMode === "system" ? "" : colorMode,
    board_theme: boardTheme === DEFAULT_BOARD_THEME ? "" : boardTheme,
    message_layout: messageLayout === "standard" ? "" : messageLayout,
    density: density === "comfortable" ? "" : density,
  };
}

export function applyServerPreferences(preferences: AppearancePreferences) {
  setLocalColorMode(preferences.color_mode || DEFAULT_COLOR_MODE);
  setLocalBoardTheme(preferences.board_theme || DEFAULT_BOARD_THEME);
  setLocalMessageLayout(preferences.message_layout || DEFAULT_MESSAGE_LAYOUT);
  setLocalDensity(preferences.density || DEFAULT_DENSITY);
}

export async function requestCurrentUser(init: RequestInit = {}): Promise<{ user: User }> {
  const requestState: AppearanceRequestState = {
    userID: appearanceSyncUserID,
    revision: appearanceRevision,
    settledRevision: appearanceSettledRevision,
    accountGeneration: appearanceAccountGeneration,
  };
  const data = await api<{ user: User }>("/api/me", init);
  reconcileAppearancePreferences(data.user, requestState);
  return data;
}

function reconcileAppearancePreferences(user: User, requestState?: AppearanceRequestState) {
  if (requestState && appearanceSyncUserID && requestState.userID !== appearanceSyncUserID) {
    return;
  }

  const sameUser = appearanceSyncUserID === user.id;
  if (
    sameUser &&
    requestState &&
    (requestState.accountGeneration !== appearanceAccountGeneration ||
      requestState.revision !== appearanceRevision ||
      requestState.revision !== requestState.settledRevision)
  ) {
    return;
  }

  if (!sameUser) {
    appearanceAccountGeneration += 1;
    appearanceRevision = 0;
    appearanceSettledRevision = 0;
  }
  appearanceSyncUserID = user.id;
  const cacheUserID = loadAppearanceCacheUser();

  if (user.appearance_preferences !== undefined) {
    applyServerPreferences(user.appearance_preferences);
    rememberAppearanceCacheUser();
    return;
  }

  if (cacheUserID && cacheUserID !== user.id) {
    applyServerPreferences({});
    rememberAppearanceCacheUser();
    return;
  }

  rememberAppearanceCacheUser();
  if (migratedAppearanceUsers.has(user.id)) return;
  migratedAppearanceUsers.add(user.id);

  const preferences = serializeAppearancePreferences();
  const patch = Object.fromEntries(
    Object.entries(preferences).filter(([, value]) => value !== ""),
  ) as AppearancePreferencesPatch;
  if (Object.keys(patch).length > 0) queueAppearancePatch(patch);
}

function queueAppearancePatch(patch: AppearancePreferencesPatch) {
  const userID = appearanceSyncUserID;
  if (!userID || Object.keys(patch).length === 0) return;
  const revision = ++appearanceRevision;
  const accountGeneration = appearanceAccountGeneration;
  appearanceWriteQueue = appearanceWriteQueue.then(async () => {
    if (appearanceSyncUserID !== userID || appearanceAccountGeneration !== accountGeneration) {
      return;
    }
    try {
      const data = await api<{ user: User }>("/api/me", {
        method: "PATCH",
        body: JSON.stringify({ appearance_preferences: patch }),
      });
      if (
        appearanceSyncUserID !== userID ||
        appearanceAccountGeneration !== accountGeneration ||
        data.user.id !== userID
      ) {
        return;
      }
      appearanceSettledRevision = Math.max(appearanceSettledRevision, revision);
      if (appearanceRevision === revision) {
        applyServerPreferences(data.user.appearance_preferences ?? {});
        rememberAppearanceCacheUser();
      }
    } catch {
      // The local cache remains active; a later change or reload can retry.
      if (appearanceSyncUserID === userID && appearanceAccountGeneration === accountGeneration) {
        appearanceSettledRevision = Math.max(appearanceSettledRevision, revision);
      }
    }
  });
}

function loadAppearanceCacheUser(): string {
  try {
    return window.localStorage.getItem(APPEARANCE_CACHE_USER_STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}

function rememberAppearanceCacheUser() {
  if (!appearanceSyncUserID) return;
  try {
    window.localStorage.setItem(APPEARANCE_CACHE_USER_STORAGE_KEY, appearanceSyncUserID);
  } catch {
    // Storage can be unavailable while the in-memory and DOM state still work.
  }
}

// Re-apply the stored prefs (mount-time belt to the app.html suspenders, and
// the recovery path when the boot script could not run).
export function initAppearance() {
  applyColorMode(loadColorMode());
  applyBoardTheme(loadBoardTheme());
  applyMessageLayout(loadMessageLayout());
  applyDensity(loadDensity());
}
