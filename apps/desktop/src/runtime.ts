import { normalizeServerURL } from "./contract";

const DESKTOP_AUTH_ATTEMPT_TTL_MS = 5 * 60 * 1000;

export type DesktopAuthAttempt = {
  serverUrl: string;
  startedAt: number;
  verifier: string;
};

type ProbeResponse = Pick<Response, "json" | "redirected" | "status" | "url">;

type OverlayWindow<T> = {
  setOverlayIcon(overlay: T | null, description: string): void;
};

export async function isValidClickClackProbeResponse(
  response: ProbeResponse,
  serverUrl: string,
): Promise<boolean> {
  const expectedURL = new URL("/readyz", normalizeServerURL(serverUrl)).toString();
  if (response.redirected || response.status !== 200 || response.url !== expectedURL) return false;
  try {
    const payload: unknown = await response.json();
    return (
      Boolean(payload) &&
      typeof payload === "object" &&
      (payload as Record<string, unknown>).status === "ready"
    );
  } catch {
    return false;
  }
}

export function nextDesktopAuthAttempt(
  current: DesktopAuthAttempt | null,
  serverUrl: string,
  verifier: string,
  now = Date.now(),
): { attempt: DesktopAuthAttempt; shouldOpen: boolean } {
  const normalizedServerUrl = normalizeServerURL(serverUrl);
  const age = current ? now - current.startedAt : Number.POSITIVE_INFINITY;
  if (current?.serverUrl === normalizedServerUrl && age >= 0 && age < DESKTOP_AUTH_ATTEMPT_TTL_MS) {
    return { attempt: current, shouldOpen: false };
  }
  return {
    attempt: { serverUrl: normalizedServerUrl, startedAt: now, verifier },
    shouldOpen: true,
  };
}

export function activeDesktopAuthAttempt(
  current: DesktopAuthAttempt | null,
  serverUrl: string,
  now = Date.now(),
): DesktopAuthAttempt | null {
  if (!current || current.serverUrl !== normalizeServerURL(serverUrl)) return null;
  const age = now - current.startedAt;
  return age >= 0 && age < DESKTOP_AUTH_ATTEMPT_TTL_MS ? current : null;
}

export function applyWindowsUnreadOverlay<T>(
  platform: NodeJS.Platform,
  window: OverlayWindow<T> | null,
  unreadCount: number,
  createOverlay: () => T,
) {
  if (platform !== "win32" || !window) return;
  const hasUnread = unreadCount > 0;
  window.setOverlayIcon(
    hasUnread ? createOverlay() : null,
    hasUnread ? `${unreadCount} unread messages` : "",
  );
}

export class RendererSignalQueue {
  private pending = false;
  private ready = false;

  beginLoading() {
    this.ready = false;
  }

  finishLoading(): boolean {
    this.ready = true;
    if (!this.pending) return false;
    this.pending = false;
    return true;
  }

  request(): boolean {
    if (this.ready) return true;
    this.pending = true;
    return false;
  }

  reset() {
    this.pending = false;
    this.ready = false;
  }
}
