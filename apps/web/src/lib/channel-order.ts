import type { User } from "./types";

type Request = (path: string, init: RequestInit) => Promise<unknown>;
const PREFIX = "clickclack:sidebar-channel-order:v1:";
const localChanges = new Map<string, string[]>();
const writes = new Map<string, Promise<void>>();

export function channelOrderStorageKey(workspaceID: string, userID: string): string {
  return `${PREFIX}${userID}:${workspaceID}`;
}

export function parseChannelOrder(raw: string | null): string[] {
  if (!raw || raw.length > 1_000_000) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) &&
      parsed.length <= 10_000 &&
      parsed.every((id) => typeof id === "string" && id.length <= 128)
      ? [...new Set(parsed)]
      : [];
  } catch {
    return [];
  }
}

function cache(key: string, order: string[]) {
  try {
    window.localStorage.setItem(key, JSON.stringify(order));
  } catch {
    // The account and in-memory order still work when storage is unavailable.
  }
}

export function receiveChannelOrder(key: string | null, raw: string | null, userID: string) {
  if (key?.startsWith(`${PREFIX}${userID}:`)) {
    localChanges.set(key, parseChannelOrder(raw));
  }
}

export function resolveChannelOrder(user: User | null, workspaceID: string): string[] {
  if (!user?.id || !workspaceID) return [];
  const key = channelOrderStorageKey(workspaceID, user.id);
  // A local edit or storage event is newer than this page's account snapshot.
  const edited = localChanges.get(key);
  if (edited) return edited;
  let local: string[] = [];
  try {
    local = parseChannelOrder(window.localStorage.getItem(key));
  } catch {
    /* Offline cache is optional. */
  }
  const server = user.sidebar_preferences?.channel_order?.[workspaceID];
  if (server === undefined) return local;
  // The account stores the first 500 IDs; preserve a larger local tail.
  const order = server.length ? [...new Set([...server, ...local])].slice(0, 10_000) : [];
  cache(key, order);
  return order;
}

export function saveChannelOrder(
  userID: string,
  workspaceID: string,
  order: string[],
  request: Request,
  isCurrentUser: () => boolean,
): Promise<void> {
  const key = channelOrderStorageKey(workspaceID, userID);
  localChanges.set(key, order);
  cache(key, order);
  // Serialize writes and skip superseded queued orders, including after failure.
  const write = (writes.get(key) ?? Promise.resolve()).then(async () => {
    if (localChanges.get(key) !== order || !isCurrentUser()) return;
    try {
      const body = JSON.stringify({
        sidebar_preferences: { channel_order: { [workspaceID]: order.slice(0, 500) } },
      });
      await request("/api/me", {
        method: "PATCH",
        keepalive: new TextEncoder().encode(body).length < 60_000,
        body,
      });
    } catch {
      // Keep the offline cache; the next reorder retries the account save.
    }
  });
  writes.set(key, write);
  void write.then(() => {
    if (writes.get(key) === write) writes.delete(key);
  });
  return write;
}
