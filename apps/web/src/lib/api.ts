export class APIError extends Error {
  constructor(
    public status: number,
    body: string,
  ) {
    super(body);
  }
}

export function readableAPIError(error: unknown, fallback: string): string {
  if (!(error instanceof Error)) return fallback;
  const message = error.message.trim();
  if (!message) return fallback;
  try {
    const body = JSON.parse(message) as { error?: unknown };
    if (typeof body.error === "string" && body.error.trim()) return body.error.trim();
  } catch {
    // Plain-text API errors are already suitable for display.
  }
  return message;
}

declare global {
  interface Window {
    __CLICKCLACK_CONFIG__?: { apiBaseUrl?: string };
  }
}

export function apiBaseURL(): string {
  if (typeof window === "undefined") return "";
  return (window.__CLICKCLACK_CONFIG__?.apiBaseUrl || "").trim().replace(/\/$/, "");
}

export function apiURL(path: string): string {
  const base = apiBaseURL();
  return base ? `${base}${path.startsWith("/") ? path : `/${path}`}` : path;
}

export function apiResourceURL(value: string): string {
  return value.startsWith("/api/") ? apiURL(value) : value;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const method = (init.method ?? "GET").toUpperCase();
  headers.set("Accept", "application/json");
  if (init.body && !(init.body instanceof FormData))
    headers.set("Content-Type", "application/json");
  if (!["GET", "HEAD", "OPTIONS", "TRACE"].includes(method)) headers.set("X-ClickClack-CSRF", "1");
  const response = await fetch(apiURL(path), { ...init, credentials: "include", headers });
  if (!response.ok) {
    throw new APIError(response.status, await response.text());
  }
  if (response.status === 204 || response.status === 205) {
    return undefined as T;
  }
  const text = await response.text();
  return text ? (JSON.parse(text) as T) : (undefined as T);
}
