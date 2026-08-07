// PROJECT LOGOS — chat substrate store
//
// chatState is a Svelte writable store that owns the full chat lifecycle:
//  workspace/channel selection, message window, send, and realtime updates.
//
// Realtime: the spec calls for a WS subscription (apps/web uses
// /api/realtime/ws).  Because the WebSocket path requires the
// ClickClackClient SDK with proper Bearer-token WS sub-protocol
// negotiation, this module implements a 10-second polling fallback.
//
// TODO(ws-real): Replace the polling loop with a proper WebSocket
// subscription via ClickClackClient.events.subscribe() once the SDK
// client is wired in with a valid Bearer token.  The WS approach
// eliminates the 10 s latency and reduces API load.

import { writable, get } from "svelte/store";
import { api, APIError, ensureSession, getApiToken, apiURL } from "./api";
import type { Workspace, Channel, Message, MessagePage } from "./types";
import type { ChatStateSnapshot, ChatStatus } from "./types";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MESSAGE_PAGE_LIMIT = 50;
const POLL_INTERVAL_MS = 10_000;

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
  };
}

export const chatState = writable<ChatStateSnapshot>(emptySnapshot());

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function update(partial: Partial<ChatStateSnapshot>) {
  chatState.update((s) => ({ ...s, ...partial }));
}

let pollTimer: ReturnType<typeof setInterval> | null = null;

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Bootstrap the chat substrate:
 *  1. Ensure an authenticated session (redirect to OAuth if needed).
 *  2. Load workspaces.
 *  3. Auto-select the first workspace + first channel and load messages.
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
      startPolling(firstWs.id, firstCh.id);
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
  stopPolling();
  update({ activeChannelId: channelId, messages: [], error: undefined });

  const s = get(chatState);
  await loadMessages(channelId);

  if (s.activeWorkspaceId) {
    startPolling(s.activeWorkspaceId, channelId);
  }
}

/**
 * Switch to a different workspace. Loads its channels and selects the first one.
 */
export async function selectWorkspace(workspaceId: string): Promise<void> {
  stopPolling();
  update({ activeWorkspaceId: workspaceId, activeChannelId: null, channels: [], messages: [] });

  const channels = await loadChannels(workspaceId);
  if (channels.length > 0) {
    await selectChannel(channels[0].id);
  }
}

// ---------------------------------------------------------------------------
// Polling (realtime fallback)
// ---------------------------------------------------------------------------

function startPolling(workspaceId: string, channelId: string): void {
  stopPolling();

  pollTimer = setInterval(async () => {
    try {
      const s = get(chatState);
      // Only poll if we're still on the same channel
      if (s.activeChannelId !== channelId || s.status !== "ready") return;

      const newestSeq = s.messages.length > 0
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

function setError(context: string, err: unknown): void {
  const message = err instanceof APIError
    ? `${context}: ${err.status} ${err.message}`
    : err instanceof Error
      ? `${context}: ${err.message}`
      : `${context}: unknown error`;
  update({ error: message });
  console.warn(`[logos/chat] ${message}`);
}
