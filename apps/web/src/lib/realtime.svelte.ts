import type { RealtimeEvent } from "./types";
import { api, apiURL } from "./api";
import { prepareRealtimeCursor, requiresRealtimeResync } from "./realtime-bootstrap";
import { createRealtimeQueue } from "./realtime-queue";

export type RealtimeOptions = {
  workspaceID: string;
  onEvent: (event: RealtimeEvent, isCurrent: () => boolean) => void | Promise<void>;
  onOpen?: (isCurrent: () => boolean, authoritativeResync: boolean) => void | Promise<void>;
  onError?: (error: unknown) => void;
  onStatusChange?: (connected: boolean) => void;
  reconnectDelayMs?: number;
};

export type RealtimeConnection = {
  readonly connected: boolean;
  close(): void;
};

type RealtimeTailResponse = {
  tail_cursor?: string;
};

const cursorKey = (workspaceID: string) => `clickclack:${workspaceID}:cursor`;

export function connectRealtime(options: RealtimeOptions): RealtimeConnection {
  const { workspaceID, onEvent, onOpen, onError, onStatusChange } = options;
  const reconnectDelayMs = options.reconnectDelayMs ?? 1200;

  let socket: WebSocket | null = null;
  let bootstrapAbort: AbortController | null = null;
  let reconnectTimer: number | undefined;
  let generation = 0;
  let memoryCursor: string | null = null;
  let storageHydrated = false;
  let storageWritable = true;
  let forceBootstrap = false;
  let pendingResyncCursor: string | null = null;
  let closed = false;
  let connected = false;

  function setConnected(next: boolean) {
    if (connected === next) return;
    connected = next;
    onStatusChange?.(next);
  }

  function reportError(error: unknown) {
    try {
      onError?.(error);
    } catch {
      // Reporting must not turn a handled connection failure into an unhandled rejection.
    }
  }

  function readCursor(): string | null {
    if (forceBootstrap) return null;
    if (memoryCursor !== null || storageHydrated) return memoryCursor;
    storageHydrated = true;
    try {
      memoryCursor = localStorage.getItem(cursorKey(workspaceID));
    } catch {
      storageWritable = false;
    }
    return memoryCursor;
  }

  function saveCursor(cursor: string) {
    memoryCursor = cursor;
    if (!storageWritable) return;
    try {
      localStorage.setItem(cursorKey(workspaceID), cursor);
    } catch {
      storageWritable = false;
    }
  }

  function isCurrentGeneration(currentGeneration: number): boolean {
    return !closed && generation === currentGeneration;
  }

  function scheduleReconnect(currentGeneration: number) {
    if (!isCurrentGeneration(currentGeneration) || reconnectTimer !== undefined) return;
    const retryGeneration = ++generation;
    bootstrapAbort?.abort();
    bootstrapAbort = null;
    setConnected(false);
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = undefined;
      if (!closed && generation === retryGeneration) open();
    }, reconnectDelayMs);
  }

  async function fetchTailCursor(signal: AbortSignal): Promise<string> {
    if (forceBootstrap && pendingResyncCursor !== null) return pendingResyncCursor;
    const params = new URLSearchParams({
      workspace_id: workspaceID,
      limit: "1",
      include_tail: "true",
    });
    const data = await api<RealtimeTailResponse>(`/api/realtime/events?${params.toString()}`, {
      signal,
    });
    if (typeof data.tail_cursor !== "string") {
      throw new Error("Realtime bootstrap response did not include a tail cursor");
    }
    if (forceBootstrap) pendingResyncCursor = data.tail_cursor;
    return data.tail_cursor;
  }

  async function openAttempt(currentGeneration: number, signal: AbortSignal) {
    const initial = await prepareRealtimeCursor({
      readCursor,
      fetchTailCursor: () => fetchTailCursor(signal),
      isActive: () => isCurrentGeneration(currentGeneration),
    });
    if (!initial.active || !isCurrentGeneration(currentGeneration)) return;

    const url = new URL(apiURL("/api/realtime/ws"), window.location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    url.searchParams.set("workspace_id", workspaceID);
    if (initial.cursor) url.searchParams.set("after_cursor", initial.cursor);

    const current = new WebSocket(url);
    socket = current;
    const isCurrent = () => isCurrentGeneration(currentGeneration) && socket === current;
    let opening = Promise.resolve();
    let failureReported = false;
    const failCurrent = (error: unknown) => {
      if (!isCurrent()) return;
      if (!failureReported) reportError(error);
      failureReported = true;
      scheduleReconnect(currentGeneration);
      current.close();
    };
    const queue = createRealtimeQueue<RealtimeEvent>({
      async onEvent(event) {
        await opening;
        if (!isCurrent()) return;
        await onEvent(event, isCurrent);
      },
      persistCursor(cursor) {
        saveCursor(cursor);
      },
      onError: failCurrent,
      isActive: isCurrent,
    });

    current.addEventListener("open", () => {
      if (!isCurrent()) return;
      opening = Promise.resolve()
        .then(() => onOpen?.(isCurrent, initial.persistAfterOpen))
        .then(() => {
          if (initial.persistAfterOpen && isCurrent()) {
            saveCursor(initial.cursor);
            forceBootstrap = false;
            pendingResyncCursor = null;
          }
        });
      void opening.then(() => {
        if (isCurrent()) setConnected(true);
      }, failCurrent);
    });

    current.addEventListener("message", (message) => {
      if (!isCurrent()) return;
      let event: RealtimeEvent;
      try {
        event = JSON.parse(String(message.data)) as RealtimeEvent;
      } catch {
        return;
      }
      if (!isRealtimeEvent(event)) return;
      queue.enqueue(event);
    });

    current.addEventListener("close", (event) => {
      queue.stop();
      if (!isCurrent()) return;
      if (requiresRealtimeResync(event.code)) {
        forceBootstrap = true;
        pendingResyncCursor = null;
      }
      socket = null;
      setConnected(false);
      scheduleReconnect(currentGeneration);
    });
  }

  function open() {
    if (closed) return;
    const currentGeneration = ++generation;
    const controller = new AbortController();
    bootstrapAbort = controller;
    void openAttempt(currentGeneration, controller.signal)
      .catch((error) => {
        if (!isCurrentGeneration(currentGeneration)) return;
        reportError(error);
        scheduleReconnect(currentGeneration);
      })
      .finally(() => {
        if (bootstrapAbort === controller) bootstrapAbort = null;
      });
  }

  open();

  return {
    get connected() {
      return connected;
    },
    close() {
      closed = true;
      generation += 1;
      bootstrapAbort?.abort();
      bootstrapAbort = null;
      setConnected(false);
      if (reconnectTimer) window.clearTimeout(reconnectTimer);
      reconnectTimer = undefined;
      socket?.close();
      socket = null;
    },
  };
}

function isRealtimeEvent(value: unknown): value is RealtimeEvent {
  return (
    typeof value === "object" && value !== null && typeof (value as RealtimeEvent).type === "string"
  );
}
