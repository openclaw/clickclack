// Appearance preferences: color mode, board theme, and message layout.
//
// All three are personal, device-local prefs stored in localStorage and
// applied as data attributes on <html>. The style sheets map those attributes
// to color, token, and layout changes. An inline script in app.html applies
// the stored values before first paint so a forced mode or non-default board
// never flashes; this module is the single writer afterwards. Keep the
// storage keys and attribute names in sync with that script.

export type ColorMode = "light" | "dark" | "system";
export type BoardTheme = "signal" | "ember" | "moss" | "iris";
export type MessageLayout = "standard" | "outlined";

export const COLOR_MODE_STORAGE_KEY = "clickclack:color-mode:v1";
export const BOARD_THEME_STORAGE_KEY = "clickclack:board-theme:v1";
export const MESSAGE_LAYOUT_STORAGE_KEY = "clickclack:message-layout:v1";

export const DEFAULT_COLOR_MODE: ColorMode = "system";
export const DEFAULT_BOARD_THEME: BoardTheme = "signal";
export const DEFAULT_MESSAGE_LAYOUT: MessageLayout = "standard";

export const COLOR_MODES: { id: ColorMode; label: string }[] = [
  { id: "light", label: "Light" },
  { id: "dark", label: "Dark" },
  { id: "system", label: "System" },
];

export const BOARD_THEMES: { id: BoardTheme; label: string; blurb: string }[] = [
  { id: "signal", label: "Signal", blurb: "Porcelain board, electric cyan" },
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

function isColorMode(value: string | null): value is ColorMode {
  return value === "light" || value === "dark" || value === "system";
}

function isBoardTheme(value: string | null): value is BoardTheme {
  return BOARD_THEMES.some((board) => board.id === value);
}

function isMessageLayout(value: string | null): value is MessageLayout {
  return MESSAGE_LAYOUTS.some((layout) => layout.id === value);
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

export function applyColorMode(mode: ColorMode) {
  try {
    if (mode === "system") document.documentElement.removeAttribute("data-color-mode");
    else document.documentElement.setAttribute("data-color-mode", mode);
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

export function setColorMode(mode: ColorMode) {
  applyColorMode(mode);
  try {
    if (mode === DEFAULT_COLOR_MODE) window.localStorage.removeItem(COLOR_MODE_STORAGE_KEY);
    else window.localStorage.setItem(COLOR_MODE_STORAGE_KEY, mode);
  } catch {
    // Ignore unavailable storage; the in-memory pref still applies this session.
  }
}

export function setBoardTheme(board: BoardTheme) {
  applyBoardTheme(board);
  try {
    if (board === DEFAULT_BOARD_THEME) window.localStorage.removeItem(BOARD_THEME_STORAGE_KEY);
    else window.localStorage.setItem(BOARD_THEME_STORAGE_KEY, board);
  } catch {
    // Ignore unavailable storage; the in-memory pref still applies this session.
  }
}

export function setMessageLayout(layout: MessageLayout) {
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

// Re-apply the stored prefs (mount-time belt to the app.html suspenders, and
// the recovery path when the boot script could not run).
export function initAppearance() {
  applyColorMode(loadColorMode());
  applyBoardTheme(loadBoardTheme());
  applyMessageLayout(loadMessageLayout());
}
