// PROJECT LOGOS — chat substrate store
//
// chatState is a Svelte writable store that owns the full chat lifecycle:
//  workspace/channel selection, message window, send, and realtime updates.
//
// Realtime: WebSocket to /api/realtime/ws with automatic fallback to 10 s
// polling.  The WS connection uses same-origin cookie auth (no Bearer-token
// sub-protocol) and auto-reconnects with exponential-ish backoff.  After 3
// consecutive WS failures the module falls back to polling and retries WS
// every 60 s.
//
// WS protocol (discovered from apps/web/src/lib/realtime.svelte.ts +
// packages/sdk-ts/src/index.ts events.subscribe()):
//   Path:   /api/realtime/ws?workspace_id=<id>[&after_cursor=<cursor>]
//   Auth:   same-origin cookie (browser sends session cookie automatically)
//   Events: JSON RealtimeEvent { id, cursor, type, workspace_id,
//           channel_id?, seq?, created_at, payload }

import { writable, get } from "svelte/store";
import { api, APIError, ensureSession, apiURL } from "./api";
import type { Workspace, Channel, Message, MessagePage, RealtimeEvent } from "./types";
import type { ChatStateSnapshot, ChatStatus } from "./types";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MESSAGE_PAGE_LIMIT = 50;
const POLL_INTERVAL_MS = 10_000;

// WebSocket backoff (ms): attempt 1→1 s, 2→2 s, 3→5 s, 4+→15 s
const WS_RECONNECT_DELAYS = [1000, 2000, 5000, 15000];
const WS_MAX_FAILURES_BEFORE_POLL = 3;
const WS_RETRY_FROM_POLL_MS = 60_000;

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

function emptySnapshot(): ChatStateSnapshot {
  return {
    status: "booting",
    workspaces: [],
    channels: [],
    activeWorkspaceId: null,
    activeChannelId: null,
    messages: [],
    realtime: "poll",
  };
}

export const chatState = writable<ChatStateSnapshot>(emptySnapshot());

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function update(partial: Partial<ChatStateSnapshot>) {
  chatState.update((s) => ({ ...s, ...partial }));
}

// ---------------------------------------------------------------------------
// Realtime state (module-private — not in the Svelte store)
// ---------------------------------------------------------------------------

let pollTimer: ReturnType<typeof setInterval> | null = null;
let wsSocket: WebSocket | null = null;
let wsReconnectTimer: ReturnType<typeof setTimeout> | null = null;
let wsFailCount = 0;
let wsGeneration = 0;
let wsWorkspaceId: string | null = null;
let wsChannelId: string | null = null;
let wsPollRetryTimer: ReturnType<typeof setTimeout> | null = null;

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Bootstrap the chat substrate:
 *  1. Ensure an authenticated session (redirect to OAuth if needed).
 *  2. Load workspaces.
 *  3. Auto-select the first workspace + first channel, load messages,
 *     and start realtime (WS first, polling fallback).
 */
export async function boot(): Promise<void> {
  update({ status: "booting" });

  const ok = await ensureSession();
  if (!ok) return; // browser is navigating away for OAuth

  try {
    const data = await api<{ workspaces: Workspace[] }>("/api/workspaces");
    const workspaces = data.workspaces;
    if (!workspaces.length) {
      update({ status: "ready", workspaces: [], channels: [], messages: [] });
      return;
    }

    const firstWs = workspaces[0];
    update({ workspaces, activeWorkspaceId: firstWs.id });

    const channels = await loadChannels(firstWs.id);
    if (channels.length > 0) {
      const firstCh = channels[0];
      update({ activeChannelId: firstCh.id });
      await loadMessages(firstCh.id);
      connectRealtime(firstWs.id, firstCh.id);
    }

    update({ status: "ready" });
  } catch (err) {
    update({
      status: "error",
      error: err instanceof Error ? err.message : "Unknown boot error",
    });
  }
}

/**
 * Load workspaces for the authenticated user.
 */
export async function loadWorkspaces(): Promise<Workspace[]> {
  try {
    const data = await api<{ workspaces: Workspace[] }>("/api/workspaces");
    update({ workspaces: data.workspaces });
    return data.workspaces;
  } catch (err) {
    setError("loadWorkspaces", err);
    return [];
  }
}

/**
 * Load channels for a workspace.
 */
export async function loadChannels(workspaceId: string): Promise<Channel[]> {
  try {
    const data = await api<{ channels: Channel[] }>(
      `/api/workspaces/${workspaceId}/channels`,
    );
    update({ channels: data.channels });
    return data.channels;
  } catch (err) {
    setError("loadChannels", err);
    return [];
  }
}

/**
 * Load messages for a channel (latest page). Replaces the current message window.
 */
export async function loadMessages(
  channelId: string,
  opts?: { before?: number; limit?: number },
): Promise<Message[]> {
  update({ activeChannelId: channelId, error: undefined });

  const params = new URLSearchParams();
  params.set("mode", "latest");
  params.set("limit", String(opts?.limit ?? MESSAGE_PAGE_LIMIT));
  if (opts?.before !== undefined) params.set("before_seq", String(opts.before));

  try {
    const page = await api<MessagePage>(
      `/api/channels/${channelId}/messages?${params.toString()}`,
    );
    update({ messages: page.messages });
    return page.messages;
  } catch (err) {
    setError("loadMessages", err);
    return [];
  }
}

/**
 * Send a message to a channel.
 */
export async function sendMessage(
  channelId: string,
  body: string,
): Promise<Message | null> {
  try {
    const data = await api<{ message: Message }>(`/api/channels/${channelId}/messages`, {
      method: "POST",
      body: JSON.stringify({ body }),
    });
    // Prepend the new message to the live window
    chatState.update((s) => ({
      ...s,
      messages: [...s.messages, data.message].slice(-200), // keep window bounded
    }));
    return data.message;
  } catch (err) {
    setError("sendMessage", err);
    return null;
  }
}

/**
 * Switch to a different channel in the active workspace.
 */
export async function selectChannel(channelId: string): Promise<void> {
  disconnectRealtime();
  update({ activeChannelId: channelId, messages: [], error: undefined });

  const s = get(chatState);
  await loadMessages(channelId);

  if (s.activeWorkspaceId) {
    connectRealtime(s.activeWorkspaceId, channelId);
  }
}

/**
 * Switch to a different workspace. Loads its channels and selects the first one.
 */
export async function selectWorkspace(workspaceId: string): Promise<void> {
  disconnectRealtime();
  update({
    activeWorkspaceId: workspaceId,
    activeChannelId: null,
    channels: [],
    messages: [],
  });

  const channels = await loadChannels(workspaceId);
  if (channels.length > 0) {
    await selectChannel(channels[0].id);
  }
}

// ---------------------------------------------------------------------------
// Realtime — WebSocket (primary) + polling (fallback)
// ---------------------------------------------------------------------------

/**
 * Fetch the server-side tail cursor so the WS subscription starts near live
 * rather than replaying the entire event history.
 */
async function fetchTailCursor(workspaceId: string): Promise<string | null> {
  try {
    const params = new URLSearchParams({
      workspace_id: workspaceId,
      limit: "1",
      include_tail: "true",
    });
    const data = await api<{ tail_cursor?: string }>(
      `/api/realtime/events?${params.toString()}`,
    );
    return typeof data.tail_cursor === "string" ? data.tail_cursor : null;
  } catch {
    return null;
  }
}

/** Tear down any existing WS + polling + timers. Idempotent. */
function disconnectRealtime(): void {
  wsGeneration += 1;
  if (wsReconnectTimer !== null) {
    clearTimeout(wsReconnectTimer);
    wsReconnectTimer = null;
  }
  if (wsPollRetryTimer !== null) {
    clearTimeout(wsPollRetryTimer);
    wsPollRetryTimer = null;
  }
  if (wsSocket) {
    wsSocket.onopen = null;
    wsSocket.onmessage = null;
    wsSocket.onclose = null;
    wsSocket.onerror = null;
    wsSocket.close();
    wsSocket = null;
  }
  wsWorkspaceId = null;
  wsChannelId = null;
  stopPolling();
}

/** Start the realtime connection: try WebSocket, fall back to polling. */
function connectRealtime(workspaceId: string, channelId: string): void {
  disconnectRealtime();

  wsWorkspaceId = workspaceId;
  wsChannelId = channelId;
  wsFailCount = 0;
  wsGeneration += 1;
  const gen = wsGeneration;

  openWebSocket(gen);
}

/**
 * Open a WebSocket to /api/realtime/ws.
 *
 * First fetches the tail cursor so we skip historical events, then opens
 * the WS.  On failure, calls onWsFailure for backoff / fallback.
 */
function openWebSocket(gen: number): void {
  if (wsGeneration !== gen) return;
  const workspaceId = wsWorkspaceId;
  const channelId = wsChannelId;
  if (!workspaceId || !channelId) return;

  // Resolve tail cursor (best-effort; don't block on failure)
  fetchTailCursor(workspaceId)
    .then((tailCursor) => ({ tailCursor }))
    .catch(() => ({ tailCursor: null as string | null }))
    .then(({ tailCursor }) => {
      if (wsGeneration !== gen) return;
      if (wsWorkspaceId !== workspaceId || wsChannelId !== channelId) return;

      const url = new URL(apiURL("/api/realtime/ws"), window.location.href);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.searchParams.set("workspace_id", workspaceId);
      if (tailCursor) url.searchParams.set("after_cursor", tailCursor);

      let socket: WebSocket;
      try {
        socket = new WebSocket(url.toString());
      } catch (err) {
        onWsFailure(
          gen,
          err instanceof Error ? err : new Error("WebSocket construction failed"),
        );
        return;
      }
      wsSocket = socket;

      socket.addEventListener("open", () => {
        if (wsGeneration !== gen || wsSocket !== socket) return;
        wsFailCount = 0;
        update({ realtime: "ws" });
      });

      socket.addEventListener("message", (event) => {
        if (wsGeneration !== gen || wsSocket !== socket) return;
        try {
          const msg = JSON.parse(String(event.data)) as RealtimeEvent;
          if (!isRealtimeEvent(msg)) return;
          handleRealtimeEvent(msg, channelId);
        } catch {
          // Malformed message — ignore
        }
      });

      socket.addEventListener("close", (_event) => {
        if (wsSocket === socket) wsSocket = null;
        if (wsGeneration !== gen) return;
        onWsFailure(gen, new Error("WebSocket closed"));
      });

      socket.addEventListener("error", () => {
        // Browser fires close after error; handle there
      });
    });
}

/** Called when the WebSocket fails to open or closes unexpectedly. */
function onWsFailure(gen: number, _error: Error): void {
  if (wsGeneration !== gen) return;
  wsFailCount++;

  if (wsFailCount >= WS_MAX_FAILURES_BEFORE_POLL) {
    // Fall back to polling
    const channelId = wsChannelId;
    if (channelId) {
      update({ realtime: "poll" });
      startPolling(channelId);
    }
    // Schedule a WS retry later
    if (wsPollRetryTimer !== null) clearTimeout(wsPollRetryTimer);
    wsPollRetryTimer = setTimeout(() => {
      wsPollRetryTimer = null;
      if (wsGeneration !== gen) return;
      wsFailCount = 0;
      stopPolling();
      openWebSocket(gen);
    }, WS_RETRY_FROM_POLL_MS);
    return;
  }

  // Backoff reconnect
  const delay =
    WS_RECONNECT_DELAYS[Math.min(wsFailCount - 1, WS_RECONNECT_DELAYS.length - 1)];
  if (wsReconnectTimer !== null) clearTimeout(wsReconnectTimer);
  wsReconnectTimer = setTimeout(() => {
    wsReconnectTimer = null;
    if (wsGeneration !== gen) return;
    openWebSocket(gen);
  }, delay);
}

/**
 * Handle a single RealtimeEvent from the WebSocket.
 *
 * For message.created on the active channel, triggers a REST fetch for the
 * new messages.  Other event types are silently ignored (LOGOS is a
 * single-channel chat view; channel lifecycle events don't apply).
 */
function handleRealtimeEvent(event: RealtimeEvent, activeChannelId: string): void {
  if (event.type !== "message.created") return;

  const payload = event.payload as Record<string, unknown> | undefined;
  const eventChannelId =
    event.channel_id ||
    (typeof payload?.channel_id === "string" ? payload.channel_id : "");
  if (eventChannelId !== activeChannelId) return;

  // Fetch new messages since our last known seq
  fetchNewMessages(activeChannelId);
}

/** Fetch messages newer than our current last seq and append to the store. */
async function fetchNewMessages(channelId: string): Promise<void> {
  const s = get(chatState);
  if (s.activeChannelId !== channelId || s.status !== "ready") return;

  const newestSeq =
    s.messages.length > 0
      ? s.messages[s.messages.length - 1].channel_seq
      : undefined;

  const params = new URLSearchParams();
  params.set("mode", "latest");
  params.set("limit", String(MESSAGE_PAGE_LIMIT));
  if (newestSeq !== undefined) params.set("after_seq", String(newestSeq));

  try {
    const page = await api<MessagePage>(
      `/api/channels/${channelId}/messages?${params.toString()}`,
    );

    if (page.messages.length > 0) {
      chatState.update((prev) => {
        if (prev.activeChannelId !== channelId) return prev;
        const existing = new Set(prev.messages.map((m) => m.id));
        const fresh = page.messages.filter((m) => !existing.has(m.id));
        if (fresh.length === 0) return prev;
        return {
          ...prev,
          messages: [...prev.messages, ...fresh].slice(-200),
        };
      });
    }
  } catch {
    // Silently ignore — a subsequent WS event will re-trigger
  }
}

// ---------------------------------------------------------------------------
// Polling (realtime fallback)
// ---------------------------------------------------------------------------

function startPolling(channelId: string): void {
  stopPolling();

  pollTimer = setInterval(async () => {
    try {
      const s = get(chatState);
      // Only poll if we're still on the same channel
      if (s.activeChannelId !== channelId || s.status !== "ready") return;

      const newestSeq =
        s.messages.length > 0
          ? s.messages[s.messages.length - 1].channel_seq
          : undefined;

      const params = new URLSearchParams();
      params.set("mode", "latest");
      params.set("limit", String(MESSAGE_PAGE_LIMIT));
      if (newestSeq !== undefined) params.set("after_seq", String(newestSeq));

      const page = await api<MessagePage>(
        `/api/channels/${channelId}/messages?${params.toString()}`,
      );

      if (page.messages.length > 0) {
        chatState.update((prev) => {
          // Deduplicate: only add messages newer than what we already have
          const existing = new Set(prev.messages.map((m) => m.id));
          const fresh = page.messages.filter((m) => !existing.has(m.id));
          if (fresh.length === 0) return prev;
          return {
            ...prev,
            messages: [...prev.messages, ...fresh].slice(-200),
          };
        });
      }
    } catch {
      // Poll failures are silent — next tick will retry
    }
  }, POLL_INTERVAL_MS);
}

function stopPolling(): void {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function isRealtimeEvent(value: unknown): value is RealtimeEvent {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as RealtimeEvent).type === "string"
  );
}

function setError(context: string, err: unknown): void {
  const message =
    err instanceof APIError
      ? `${context}: ${err.status} ${err.message}`
      : err instanceof Error
        ? `${context}: ${err.message}`
        : `${context}: unknown error`;
  update({ error: message });
  console.warn(`[logos/chat] ${message}`);
}
