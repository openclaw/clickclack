// PROJECT LOGOS — minimal typed clickclack substrate client
//
// Two auth lanes, tried in order:
//  1. Cookie (same-origin /api, credentials: "include") — works after GitHub OAuth redirect.
//  2. Bearer token from localStorage ("logos_api_token") — token-based auth fallback.
//
// Session flow (mirrors apps/web pattern):
//  - getSession() → GET /api/me with cookie → returns { user } or null
//  - ensureSession() → if no session, redirect browser to /api/auth/github/start
//  - after OAuth callback the server sets a session cookie; caller re-checks getSession()
//
// api<T>(path, init) is the one typed fetch helper: JSON request/response, error handling,
// CSRF header on mutating verbs, and optional Bearer token for token-based lanes.

import type { Session } from "./types";

// ---------------------------------------------------------------------------
// URL helpers
// ---------------------------------------------------------------------------

declare global {
  interface Window {
    __CLICKCLACK_CONFIG__?: { apiBaseUrl?: string };
  }
}

const LS_TOKEN_KEY = "logos_api_token";
const LS_USER_KEY = "logos_user";

export function apiBaseURL(): string {
  if (typeof window === "undefined") return "";
  return (window.__CLICKCLACK_CONFIG__?.apiBaseUrl || "").trim().replace(/\/$/, "");
}

export function apiURL(path: string): string {
  const base = apiBaseURL();
  return base ? `${base}${path.startsWith("/") ? path : `/${path}`}` : path;
}

// ---------------------------------------------------------------------------
// Typed fetch helper
// ---------------------------------------------------------------------------

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
    // plain-text errors are already suitable
  }
  return message;
}

/**
 * Typed fetch against the clickclack API (same-origin /api).
 * Uses credentials: "include" for cookie auth; adds Bearer token if
 * a logos_api_token is in localStorage (token-based lane override).
 */
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const method = (init.method ?? "GET").toUpperCase();
  headers.set("Accept", "application/json");
  if (init.body && !(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  if (!["GET", "HEAD", "OPTIONS", "TRACE"].includes(method)) {
    headers.set("X-ClickClack-CSRF", "1");
  }
  // Token-based auth lane (override cookie when present)
  try {
    const stored = localStorage.getItem(LS_TOKEN_KEY);
    if (stored) headers.set("Authorization", `Bearer ${stored}`);
  } catch {
    // localStorage unavailable (SSR or privacy block)
  }

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

// ---------------------------------------------------------------------------
// Auth / session helpers
// ---------------------------------------------------------------------------

/**
 * Try to resolve the current session:
 *  1. Call GET /api/me (cookie auth).
 *  2. If it fails with 401, return null.
 *
 * On success the caller receives the user object.  If the server also
 * returns a session token (e.g. from the OAuth callback redirect) we
 * persist it in localStorage so the Bearer lane is active going forward.
 */
export async function getSession(): Promise<Session | null> {
  try {
    const data = await api<{ user: import("@clickclack/sdk-ts").User }>("/api/me");
    const user = data.user;
    if (!user?.id) return null;

    // Cache user in localStorage for offline / diagnostic use
    try {
      localStorage.setItem(LS_USER_KEY, JSON.stringify({ id: user.id, handle: user.handle }));
    } catch {
      // noop
    }

    return { user };
  } catch (err) {
    // 401 = unauthenticated; network failures also degrade to null
    if (err instanceof APIError && err.status === 401) {
      // clear stale local data
      try {
        localStorage.removeItem(LS_TOKEN_KEY);
        localStorage.removeItem(LS_USER_KEY);
      } catch {
        // noop
      }
    }
    return null;
  }
}

/**
 * Ensure a valid session exists. If not authenticated, redirect the
 * browser to the GitHub OAuth start URL (same-origin /api/auth/github/start).
 *
 * After the user completes OAuth the server sets a session cookie and
 * redirects back to the app.  Callers should re-run getSession() on the
 * next page load / component mount.
 *
 * Returns `true` if the session is valid; otherwise the browser is
 * navigating away and this function never returns `false` synchronously.
 */
export async function ensureSession(): Promise<boolean> {
  const session = await getSession();
  if (session) return true;

  // Redirect to GitHub OAuth — same-origin, browser navigates away.
  // Pass return_to=/logos/ so the callback lands back inside the LOGOS app
  // instead of the clickclack root (server honors it via cookie).
  const base = window.location.pathname.startsWith("/logos") ? "/logos/" : "/";
  window.location.href = apiURL(`/api/auth/github/start?return_to=${encodeURIComponent(base)}`);
  // The browser will navigate away; return value is never observed.
  return false;
}

/**
 * Store an API token in localStorage for Bearer-auth lane.
 * Use this if the application mints its own token (e.g. after OAuth
 * callback or through the clickclack API token management).
 */
export function setApiToken(token: string): void {
  try {
    localStorage.setItem(LS_TOKEN_KEY, token);
  } catch {
    // noop
  }
}

/**
 * Retrieve the stored API token, if any.
 */
export function getApiToken(): string | null {
  try {
    return localStorage.getItem(LS_TOKEN_KEY);
  } catch {
    return null;
  }
}

/**
 * Clear all stored auth state (logout).
 */
export function clearAuth(): void {
  try {
    localStorage.removeItem(LS_TOKEN_KEY);
    localStorage.removeItem(LS_USER_KEY);
  } catch {
    // noop
  }
}
