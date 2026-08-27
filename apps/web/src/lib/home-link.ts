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
  if (value.startsWith("/")) return true;
  try {
    const parsed = new URL(value);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.host !== "";
  } catch {
    return false;
  }
}

/** Accept only a safe, well-formed payload; anything else falls back to the default. */
export function normalizeHomeLink(value: unknown): HomeLink {
  if (!value || typeof value !== "object") return DEFAULT_HOME_LINK;
  const record = value as Record<string, unknown>;
  const url = typeof record.url === "string" ? record.url.trim() : "";
  const label = typeof record.label === "string" ? record.label.trim() : "";
  return {
    url: url && isAllowedHomeURL(url) ? url : DEFAULT_HOME_LINK.url,
    label:
      label && Array.from(label).length <= MAX_HOME_LABEL_LENGTH ? label : DEFAULT_HOME_LINK.label,
  };
}

export function isDefaultHomeLink(link: HomeLink): boolean {
  return link.url === DEFAULT_HOME_LINK.url && link.label === DEFAULT_HOME_LINK.label;
}

export function homeLinkTitle(link: HomeLink): string {
  return isDefaultHomeLink(link) ? "ClickClack home" : `${link.label} home`;
}

/** The endpoint is public and tiny; a failure must never block the shell. */
export async function loadHomeLink(
  fetchJSON: (path: string) => Promise<unknown>,
): Promise<HomeLink> {
  try {
    return normalizeHomeLink(await fetchJSON("/api/home-link"));
  } catch {
    return DEFAULT_HOME_LINK;
  }
}
