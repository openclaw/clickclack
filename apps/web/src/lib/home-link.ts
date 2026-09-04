/**
 * Destination and label of the workspace rail's home button. Deployments that
 * live inside a larger product point it at that product through
 * CLICKCLACK_HOME_URL / CLICKCLACK_HOME_LABEL; everything else keeps the
 * ClickClack landing page.
 */
export type HomeLink = { url: string; label: string };

export const DEFAULT_HOME_LINK: HomeLink = { url: "/", label: "cc" };
export const MAX_HOME_LABEL_LENGTH = 32;

function isAllowedHomeURL(value: string): boolean {
  if (value.startsWith("//")) return false;
  try {
    if (value.startsWith("/")) {
      const base = "https://clickclack.invalid";
      return new URL(value, base).origin === base;
    }
    const parsed = new URL(value);
    return (
      (parsed.protocol === "http:" || parsed.protocol === "https:") &&
      parsed.host !== "" &&
      !parsed.username &&
      !parsed.password
    );
  } catch {
    return false;
  }
}

/** Accept only a safe, well-formed payload; anything else falls back to the default. */
export function normalizeHomeLink(value: unknown): HomeLink {
  if (!value || typeof value !== "object") return DEFAULT_HOME_LINK;
  const record = value as Record<string, unknown>;
  const rawURL = typeof record.url === "string" ? record.url : "";
  const url = rawURL.trim();
  const label = typeof record.label === "string" ? record.label.trim() : "";
  // Browsers remove controls or reinterpret backslashes before URL parsing.
  // eslint-disable-next-line no-control-regex
  const hasUnsafeCharacters = /[\u0000-\u001f\u007f\\]/u.test(rawURL);
  return {
    url: url && !hasUnsafeCharacters && isAllowedHomeURL(url) ? url : DEFAULT_HOME_LINK.url,
    label:
      label && Array.from(label).length <= MAX_HOME_LABEL_LENGTH ? label : DEFAULT_HOME_LINK.label,
  };
}

/** The default label renders as the ClickClack logo mark rather than as text. */
export function isDefaultHomeLabel(label: string): boolean {
  return label === DEFAULT_HOME_LINK.label;
}

export function homeLinkTitle(link: HomeLink): string {
  return isDefaultHomeLabel(link.label) ? "ClickClack home" : `${link.label} home`;
}

/** Older servers lack this endpoint; a failure must never block the shell. */
export async function loadHomeLink(
  fetchJSON: (path: string) => Promise<unknown>,
): Promise<HomeLink> {
  try {
    return normalizeHomeLink(await fetchJSON("/api/home-link"));
  } catch {
    return DEFAULT_HOME_LINK;
  }
}
