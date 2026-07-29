type EmbedThemeMode = "light" | "dark";

type EmbedThemeMessage = {
  type: "openclaw:widget-theme";
  mode: EmbedThemeMode;
  tokens: Record<string, unknown>;
};

type EmbedThemeLocation = Pick<Location, "pathname" | "search">;

const EMBED_ROUTE = /^\/embed\/(?:channel|thread)\//u;

const COLOR_TOKEN_TARGETS: Record<string, readonly string[]> = {
  surface: ["--bg", "--rail"],
  card: ["--panel"],
  elevated: ["--panel-2", "--panel-3"],
  text: ["--text"],
  "text-strong": ["--text-strong"],
  muted: ["--muted", "--muted-2"],
  border: ["--line"],
  "border-strong": ["--line-strong"],
  accent: ["--accent", "--brand-a"],
  "accent-fill": ["--accent-hover", "--brand-b"],
  "accent-fg": ["--accent-contrast", "--brand-contrast"],
  ok: ["--success"],
  warn: ["--warn"],
  danger: ["--danger"],
  info: ["--info"],
};

const MANAGED_CUSTOM_PROPERTIES = [
  ...new Set(Object.values(COLOR_TOKEN_TARGETS).flat()),
  "--hover",
  "--hover-strong",
  "--accent-soft",
  "--font-display",
  "--font-mono",
  "--radius-sm",
  "--radius",
  "--radius-lg",
  "--radius-xl",
] as const;

let activeEmbedThemeMode: EmbedThemeMode | null = null;

function isEmbedThemeMode(value: unknown): value is EmbedThemeMode {
  return value === "light" || value === "dark";
}

export function resolveEmbedThemeMode(location: EmbedThemeLocation): EmbedThemeMode | null {
  if (!EMBED_ROUTE.test(location.pathname)) return null;
  const mode = new URLSearchParams(location.search).get("theme");
  return isEmbedThemeMode(mode) ? mode : null;
}

export function resolveEmbedHostOrigin(location: EmbedThemeLocation): string | null {
  if (!EMBED_ROUTE.test(location.pathname)) return null;
  const value = new URLSearchParams(location.search).get("hostOrigin");
  if (!value) return null;
  try {
    const url = new URL(value);
    if ((url.protocol !== "https:" && url.protocol !== "http:") || url.origin !== value) {
      return null;
    }
    return url.origin;
  } catch {
    return null;
  }
}

export function getEmbedHostThemeMode(): EmbedThemeMode | null {
  if (typeof window === "undefined" || !EMBED_ROUTE.test(window.location.pathname)) return null;
  return activeEmbedThemeMode ?? resolveEmbedThemeMode(window.location);
}

export function clearEmbedHostTheme(): boolean {
  if (activeEmbedThemeMode === null) return false;

  activeEmbedThemeMode = null;
  const root = document.documentElement;
  for (const property of MANAGED_CUSTOM_PROPERTIES) {
    root.style.removeProperty(property);
  }
  root.style.removeProperty("font-family");
  return true;
}

function parseEmbedThemeMessage(value: unknown): EmbedThemeMessage | null {
  if (!value || typeof value !== "object") return null;
  const message = value as Partial<EmbedThemeMessage>;
  if (
    message.type !== "openclaw:widget-theme" ||
    !isEmbedThemeMode(message.mode) ||
    !message.tokens ||
    typeof message.tokens !== "object" ||
    Array.isArray(message.tokens)
  ) {
    return null;
  }
  return message as EmbedThemeMessage;
}

function supportedCssValue(property: string, value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= 512 &&
    typeof CSS !== "undefined" &&
    CSS.supports(property, value)
  );
}

function applyEmbedTheme(message: EmbedThemeMessage): void {
  const root = document.documentElement;
  activeEmbedThemeMode = message.mode;
  root.setAttribute("data-color-mode", message.mode);

  // Every update is a complete snapshot. Clearing the previous palette keeps
  // custom-to-default switches from retaining another host theme's colors.
  for (const property of MANAGED_CUSTOM_PROPERTIES) {
    root.style.removeProperty(property);
  }

  for (const [token, targets] of Object.entries(COLOR_TOKEN_TARGETS)) {
    const value = message.tokens[token];
    if (!supportedCssValue("color", value)) continue;
    for (const property of targets) root.style.setProperty(property, value);
  }

  const text = message.tokens.text;
  if (supportedCssValue("color", text)) {
    root.style.setProperty("--hover", `color-mix(in srgb, ${text} 5%, transparent)`);
    root.style.setProperty("--hover-strong", `color-mix(in srgb, ${text} 8%, transparent)`);
  }

  const accent = message.tokens.accent;
  if (supportedCssValue("color", accent)) {
    root.style.setProperty("--accent-soft", `color-mix(in srgb, ${accent} 14%, transparent)`);
  }

  const bodyFont = message.tokens["font-body"];
  if (supportedCssValue("font-family", bodyFont)) {
    root.style.fontFamily = bodyFont;
    root.style.setProperty("--font-display", bodyFont);
  } else {
    root.style.removeProperty("font-family");
  }

  const monoFont = message.tokens["font-mono"];
  if (supportedCssValue("font-family", monoFont)) {
    root.style.setProperty("--font-mono", monoFont);
  }

  const radius = message.tokens.radius;
  if (supportedCssValue("border-radius", radius)) {
    root.style.setProperty("--radius", radius);
    root.style.setProperty("--radius-sm", `calc(${radius} * 0.67)`);
    root.style.setProperty("--radius-lg", `calc(${radius} * 1.67)`);
    root.style.setProperty("--radius-xl", `calc(${radius} * 2.33)`);
  }
}

export function installEmbedHostTheme(): () => void {
  if (typeof window === "undefined") return () => {};
  const hostOrigin = resolveEmbedHostOrigin(window.location);
  if (!hostOrigin || window.parent === window) return () => {};

  activeEmbedThemeMode = resolveEmbedThemeMode(window.location);

  const onMessage = (event: MessageEvent<unknown>) => {
    // Only the exact host named in the embed URL can update this document.
    // Rechecking the current route prevents a persistent root-layout listener
    // from carrying host control into normal pages after client-side navigation.
    if (
      resolveEmbedHostOrigin(window.location) !== hostOrigin ||
      event.origin !== hostOrigin ||
      event.source !== window.parent
    ) {
      return;
    }
    const message = parseEmbedThemeMessage(event.data);
    if (message) applyEmbedTheme(message);
  };

  window.addEventListener("message", onMessage);
  return () => window.removeEventListener("message", onMessage);
}
