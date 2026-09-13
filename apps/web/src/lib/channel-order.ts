// Personal sidebar channel order.
//
// The order roams with the account. localStorage remains the pre-paint cache
// and the offline fallback, so a reorder takes effect before any request goes
// out and survives a server that cannot be reached. On load the account's saved
// order wins over the cache and is written back into it; a workspace this
// session has already reordered, or whose cache another tab has just rewritten,
// keeps its local order.
//
// Three rules keep the two copies from fighting each other. An account snapshot
// is applied at most once per user object and workspace, so returning to a
// workspace re-resolves from the cache rather than replaying a boot-time
// snapshot over a newer shared one. Account writes are serialized per scope,
// with only the newest order surviving a wait, so an older body can never land
// after a newer one. And the account list, which is capped for the wire, leads
// the local list rather than replacing it, so positions past the cap stay on
// the device that made them.
//
// The account write is injected rather than imported so this module depends on
// nothing but types. Callers pass the API helper.

import type { User } from "./types";

export type ChannelOrderRequest = (path: string, init: RequestInit) => Promise<unknown>;

export type ChannelOrderPatchBody = {
  sidebar_preferences: { channel_order: Record<string, readonly string[]> };
};

export const CHANNEL_ORDER_STORAGE_PREFIX = "clickclack:sidebar-channel-order:v1:";
export const MAX_CHANNEL_ORDER_STORAGE_LENGTH = 1_000_000;
export const MAX_CHANNEL_ORDER_IDS = 10_000;
export const MAX_CHANNEL_ID_LENGTH = 128;

// The account copy is bounded by the protocol. A longer local order still
// applies on this device; the leading window is what roams.
export const MAX_ROAMING_CHANNEL_ORDER_IDS = 500;

export const CHANNEL_ORDER_PATCH_DEBOUNCE_MS = 400;

type PendingChannelOrderPatch = {
  timer: ReturnType<typeof setTimeout>;
  body: ChannelOrderPatchBody;
};

type QueuedChannelOrderPatch = {
  body: ChannelOrderPatchBody;
  keepalive: boolean;
};

// One entry per scope with a request in flight. queued holds the order that
// arrived while that request was outstanding, latest only.
type ChannelOrderSendSlot = {
  queued?: QueuedChannelOrderPatch;
};

const pendingPatches = new Map<string, PendingChannelOrderPatch>();
const sendSlots = new Map<string, ChannelOrderSendSlot>();

// Scopes whose cache this session knows to be newer than the account snapshot
// it booted with: a local reorder, or a cache write another tab broadcast.
const locallyNewerScopes = new Set<string>();

// Workspaces whose account snapshot has already been applied, keyed by the user
// object that carried it. A fresh /api/me produces a new object and may apply
// again; the same object never applies twice.
const appliedSnapshots = new WeakMap<User, Set<string>>();

function cacheScope(workspaceID: string, userID: string): string {
  return `${userID}:${workspaceID}`;
}

export function channelOrderStorageKey(workspaceID: string, userID: string): string {
  return `${CHANNEL_ORDER_STORAGE_PREFIX}${userID}:${workspaceID}`;
}

// channelOrderWorkspaceFromStorageKey names the workspace a storage event
// belongs to, or null when the key is not this user's channel order.
export function channelOrderWorkspaceFromStorageKey(
  key: string | null,
  userID: string,
): string | null {
  if (!key || !userID) return null;
  const prefix = `${CHANNEL_ORDER_STORAGE_PREFIX}${userID}:`;
  if (!key.startsWith(prefix)) return null;
  return key.slice(prefix.length) || null;
}

export function parseChannelOrder(raw: string | null): string[] {
  if (!raw || raw.length > MAX_CHANNEL_ORDER_STORAGE_LENGTH) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) &&
      parsed.length <= MAX_CHANNEL_ORDER_IDS &&
      parsed.every((id) => typeof id === "string" && id.length <= MAX_CHANNEL_ID_LENGTH)
      ? [...new Set(parsed)]
      : [];
  } catch {
    return [];
  }
}

export function loadChannelOrder(workspaceID: string, userID: string): string[] {
  if (!workspaceID || !userID) return [];
  try {
    return parseChannelOrder(
      window.localStorage.getItem(channelOrderStorageKey(workspaceID, userID)),
    );
  } catch {
    return [];
  }
}

function writeChannelOrderCache(workspaceID: string, userID: string, order: string[]) {
  if (!workspaceID || !userID) return;
  try {
    const key = channelOrderStorageKey(workspaceID, userID);
    if (order.length === 0) {
      window.localStorage.removeItem(key);
      return;
    }
    const serialized = JSON.stringify(order);
    if (serialized.length > MAX_CHANNEL_ORDER_STORAGE_LENGTH) {
      window.localStorage.removeItem(key);
      return;
    }
    window.localStorage.setItem(key, serialized);
  } catch {
    // Storage is an enhancement; reordering still works for this session.
  }
}

// sanitizeServerChannelOrder applies the same shape rules to the account copy
// that the cache gets, so a hand-written API value cannot widen what the
// sidebar trusts.
function sanitizeServerChannelOrder(order: readonly string[]): string[] {
  const seen = new Set<string>();
  for (const id of order) {
    if (typeof id !== "string" || !id || id.length > MAX_CHANNEL_ID_LENGTH) continue;
    if (seen.size >= MAX_CHANNEL_ORDER_IDS) break;
    seen.add(id);
  }
  return [...seen];
}

// mergeChannelOrder resolves one workspace. No account order at all leaves the
// cache alone, which is what an older server and an offline first paint both
// look like. A cleared account order, the empty list, clears the cache too.
//
// Otherwise the account order leads and the local ids it does not name follow
// in their local order. The account copy stops at MAX_ROAMING_CHANNEL_ORDER_IDS
// on the way out, so replacing the local list with it would throw away every
// position past that cap on the one device that has them.
export function mergeChannelOrder(
  serverOrder: readonly string[] | undefined,
  localOrder: readonly string[],
): string[] {
  if (serverOrder === undefined) return [...localOrder];
  const account = sanitizeServerChannelOrder(serverOrder);
  if (account.length === 0) return [];
  const taken = new Set(account);
  const merged = [...account];
  for (const id of localOrder) {
    if (merged.length >= MAX_CHANNEL_ORDER_IDS) break;
    if (typeof id !== "string" || !id || id.length > MAX_CHANNEL_ID_LENGTH) continue;
    if (taken.has(id)) continue;
    taken.add(id);
    merged.push(id);
  }
  return merged;
}

export function serverChannelOrder(
  user: User | null,
  workspaceID: string,
): readonly string[] | undefined {
  if (!workspaceID) return undefined;
  const order = user?.sidebar_preferences?.channel_order?.[workspaceID];
  return Array.isArray(order) ? order : undefined;
}

// markChannelOrderLocallyNewer records that this session's cache for a scope is
// ahead of the account snapshot it booted with. A local reorder does this; so
// does a storage event, which means another tab of this browser wrote a newer
// order that this tab's boot snapshot must not overwrite.
export function markChannelOrderLocallyNewer(workspaceID: string, userID: string) {
  if (!workspaceID || !userID) return;
  locallyNewerScopes.add(cacheScope(workspaceID, userID));
}

function accountSnapshotApplied(user: User, workspaceID: string): boolean {
  return appliedSnapshots.get(user)?.has(workspaceID) === true;
}

function markAccountSnapshotApplied(user: User, workspaceID: string) {
  const applied = appliedSnapshots.get(user);
  if (applied) applied.add(workspaceID);
  else appliedSnapshots.set(user, new Set([workspaceID]));
}

// resolveChannelOrder produces the order to render and refreshes the cache from
// the account. The account snapshot applies once per user object and workspace:
// switching workspaces and coming back re-reads the cache instead of replaying
// a snapshot that may now be older than what another tab saved.
export function resolveChannelOrder(user: User | null, workspaceID: string): string[] {
  const userID = user?.id || "";
  if (!user || !workspaceID || !userID) return [];
  const local = loadChannelOrder(workspaceID, userID);
  if (locallyNewerScopes.has(cacheScope(workspaceID, userID))) return local;
  const server = serverChannelOrder(user, workspaceID);
  if (server === undefined) return mergeChannelOrder(undefined, local);
  if (accountSnapshotApplied(user, workspaceID)) return local;
  markAccountSnapshotApplied(user, workspaceID);
  const merged = mergeChannelOrder(server, local);
  writeChannelOrderCache(workspaceID, userID, merged);
  return merged;
}

// storeChannelOrder records a reorder: the cache first so the sidebar never
// waits on the network, then a debounced account write.
export function storeChannelOrder(
  workspaceID: string,
  userID: string,
  order: string[],
  request: ChannelOrderRequest,
) {
  if (!workspaceID || !userID) return;
  markChannelOrderLocallyNewer(workspaceID, userID);
  writeChannelOrderCache(workspaceID, userID, order);
  queueChannelOrderPatch(workspaceID, userID, order, request);
}

export function channelOrderPatchBody(
  workspaceID: string,
  order: readonly string[],
): ChannelOrderPatchBody {
  return {
    sidebar_preferences: {
      channel_order: { [workspaceID]: order.slice(0, MAX_ROAMING_CHANNEL_ORDER_IDS) },
    },
  };
}

export function queueChannelOrderPatch(
  workspaceID: string,
  userID: string,
  order: string[],
  request: ChannelOrderRequest,
) {
  if (!workspaceID || !userID) return;
  const key = cacheScope(workspaceID, userID);
  const pending = pendingPatches.get(key);
  if (pending) clearTimeout(pending.timer);
  const body = channelOrderPatchBody(workspaceID, order);
  const timer = setTimeout(() => {
    pendingPatches.delete(key);
    sendChannelOrderForScope(key, request, body, false);
  }, CHANNEL_ORDER_PATCH_DEBOUNCE_MS);
  pendingPatches.set(key, { timer, body });
}

// flushChannelOrderPatches sends every debounced write immediately, for the
// moment the page is going away. keepalive lets the request outlive the
// document, so a reorder made just before a tab closes still roams. A flush
// joins the scope's queue rather than racing whatever is already in flight.
export function flushChannelOrderPatches(request: ChannelOrderRequest) {
  if (pendingPatches.size === 0) return;
  const flushing = [...pendingPatches.entries()];
  pendingPatches.clear();
  for (const [key, pending] of flushing) {
    clearTimeout(pending.timer);
    sendChannelOrderForScope(key, request, pending.body, true);
  }
}

// sendChannelOrderForScope keeps one request in flight per scope. A body that
// arrives during a send waits for it and replaces any other waiting body, so
// the newest order is the one that lands and an older one can never overwrite
// it by finishing last.
function sendChannelOrderForScope(
  key: string,
  request: ChannelOrderRequest,
  body: ChannelOrderPatchBody,
  keepalive: boolean,
) {
  const slot = sendSlots.get(key);
  if (slot) {
    slot.queued = { body, keepalive };
    return;
  }
  startChannelOrderSend(key, request, body, keepalive);
}

function startChannelOrderSend(
  key: string,
  request: ChannelOrderRequest,
  body: ChannelOrderPatchBody,
  keepalive: boolean,
) {
  const slot: ChannelOrderSendSlot = {};
  sendSlots.set(key, slot);
  // sendChannelOrderPatch absorbs its own failures, so a send that fails still
  // releases the scope and lets the next order through.
  void sendChannelOrderPatch(request, body, keepalive).then(() => {
    sendSlots.delete(key);
    const next = slot.queued;
    if (next) startChannelOrderSend(key, request, next.body, next.keepalive);
  });
}

async function sendChannelOrderPatch(
  request: ChannelOrderRequest,
  body: ChannelOrderPatchBody,
  keepalive: boolean,
) {
  try {
    await request("/api/me", {
      method: "PATCH",
      body: JSON.stringify(body),
      ...(keepalive ? { keepalive: true } : {}),
    });
  } catch (error) {
    // Best effort. The order stays on this device and the next reorder retries.
    console.warn("channel order save failed", error);
  }
}
