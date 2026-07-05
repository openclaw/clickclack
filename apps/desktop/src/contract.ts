export const DEFAULT_SERVER_URL = "https://app.clickclack.chat";
export const DEFAULT_APP_ROUTE = "/app";
export const DESKTOP_SERVER_ORIGIN_ARG = "--clickclack-server-origin=";

export type WindowState = {
  height?: number;
  maximized?: boolean;
  width?: number;
  x?: number;
  y?: number;
};

export type DesktopSettings = {
  closeToTray: boolean;
  serverUrl: string;
  startAtLogin: boolean;
  window?: WindowState;
};

export type PublicDesktopSettings = Omit<DesktopSettings, "window">;

export type DesktopNotification = {
  body: string;
  route?: string;
  tag?: string;
  title: string;
};

export const defaultSettings = (): DesktopSettings => ({
  closeToTray: true,
  serverUrl: DEFAULT_SERVER_URL,
  startAtLogin: false,
});

export function normalizeServerURL(input: string): string {
  const raw = input.trim();
  if (!raw) throw new Error("Enter a ClickClack server URL");
  let value: URL;
  try {
    value = new URL(raw);
  } catch {
    throw new Error("Enter a complete http:// or https:// URL");
  }
  if (value.protocol !== "https:" && value.protocol !== "http:") {
    throw new Error("ClickClack servers must use http:// or https://");
  }
  if (value.username || value.password) {
    throw new Error("Server URLs cannot contain credentials");
  }
  const loopbackHosts = new Set(["localhost", "127.0.0.1", "::1", "[::1]"]);
  if (value.protocol === "http:" && !loopbackHosts.has(value.hostname.toLowerCase())) {
    throw new Error("Remote ClickClack servers must use HTTPS");
  }
  if (value.pathname !== "/" && value.pathname !== "/app" && value.pathname !== "/app/") {
    throw new Error("Use the server origin, without an extra path");
  }
  if (value.search || value.hash) {
    throw new Error("Server URLs cannot contain a query or fragment");
  }
  return value.origin;
}

export function safeAppRoute(input: string | undefined): string | null {
  if (!input || input.includes("\\") || input.includes("\0")) return null;
  let value: URL;
  try {
    value = new URL(input, "https://clickclack.invalid");
  } catch {
    return null;
  }
  if (value.origin !== "https://clickclack.invalid") return null;
  if (value.pathname !== "/app" && !value.pathname.startsWith("/app/")) return null;
  return `${value.pathname}${value.search}${value.hash}`;
}

export function appURL(serverUrl: string, route = DEFAULT_APP_ROUTE): string {
  const origin = normalizeServerURL(serverUrl);
  const safeRoute = safeAppRoute(route);
  if (!safeRoute) throw new Error("Invalid ClickClack app route");
  return new URL(safeRoute, origin).toString();
}

export function desktopOAuthStartURL(serverUrl: string, codeChallenge: string): string {
  if (!/^[A-Za-z0-9_-]{43}$/.test(codeChallenge)) {
    throw new Error("Invalid desktop OAuth code challenge");
  }
  const value = new URL("/api/auth/github/desktop/start", normalizeServerURL(serverUrl));
  value.searchParams.set("code_challenge", codeChallenge);
  return value.toString();
}

export function desktopOAuthCallbackCode(input: string): string | null {
  let value: URL;
  try {
    value = new URL(input);
  } catch {
    return null;
  }
  if (
    value.protocol !== "clickclack:" ||
    value.hostname !== "auth" ||
    value.pathname !== "/callback"
  ) {
    return null;
  }
  const code = value.searchParams.get("code") ?? "";
  return /^[a-f0-9]{32}$/.test(code) ? code : null;
}

export function desktopBridgeAllowed(currentOrigin: string, trustedOrigin: string | undefined) {
  if (!trustedOrigin) return false;
  try {
    return new URL(currentOrigin).origin === normalizeServerURL(trustedOrigin);
  } catch {
    return false;
  }
}

export function deepLinkToRoute(input: string): string | null {
  let value: URL;
  try {
    value = new URL(input);
  } catch {
    return null;
  }
  if (value.protocol !== "clickclack:") return null;
  if (value.hostname === "app") {
    return safeAppRoute(`/app${value.pathname}${value.search}${value.hash}`);
  }
  if (value.hostname === "open") {
    return safeAppRoute(value.searchParams.get("path") ?? value.pathname);
  }
  return null;
}

export function clampUnreadCount(input: unknown): number {
  const count = typeof input === "number" ? input : Number(input);
  if (!Number.isFinite(count)) return 0;
  return Math.max(0, Math.min(9999, Math.floor(count)));
}

export function sanitizeNotification(input: unknown): DesktopNotification | null {
  if (!input || typeof input !== "object") return null;
  const record = input as Record<string, unknown>;
  const title = typeof record.title === "string" ? record.title.trim().slice(0, 160) : "";
  const body = typeof record.body === "string" ? record.body.trim().slice(0, 500) : "";
  if (!title || !body) return null;
  const route =
    typeof record.route === "string" ? (safeAppRoute(record.route) ?? undefined) : undefined;
  const tag = typeof record.tag === "string" ? record.tag.trim().slice(0, 200) : undefined;
  return { body, route, tag, title };
}

export function mergeSettings(input: unknown): DesktopSettings {
  const defaults = defaultSettings();
  if (!input || typeof input !== "object") return defaults;
  const record = input as Record<string, unknown>;
  let serverUrl = defaults.serverUrl;
  if (typeof record.serverUrl === "string") {
    try {
      serverUrl = normalizeServerURL(record.serverUrl);
    } catch {
      // Keep the safe default when a config file was edited or corrupted.
    }
  }
  const window = normalizeWindowState(record.window);
  return {
    closeToTray:
      typeof record.closeToTray === "boolean" ? record.closeToTray : defaults.closeToTray,
    serverUrl,
    startAtLogin:
      typeof record.startAtLogin === "boolean" ? record.startAtLogin : defaults.startAtLogin,
    ...(window ? { window } : {}),
  };
}

function normalizeWindowState(input: unknown): WindowState | undefined {
  if (!input || typeof input !== "object") return undefined;
  const record = input as Record<string, unknown>;
  const width = boundedInteger(record.width, 760, 5000);
  const height = boundedInteger(record.height, 560, 5000);
  const x = boundedInteger(record.x, -20_000, 20_000);
  const y = boundedInteger(record.y, -20_000, 20_000);
  const maximized = typeof record.maximized === "boolean" ? record.maximized : undefined;
  if (
    width === undefined &&
    height === undefined &&
    x === undefined &&
    y === undefined &&
    maximized === undefined
  ) {
    return undefined;
  }
  return { height, maximized, width, x, y };
}

function boundedInteger(input: unknown, minimum: number, maximum: number): number | undefined {
  if (typeof input !== "number" || !Number.isFinite(input)) return undefined;
  const value = Math.floor(input);
  return value >= minimum && value <= maximum ? value : undefined;
}
