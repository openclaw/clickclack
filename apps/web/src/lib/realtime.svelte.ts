import type { RealtimeEvent } from "./types";

export type RealtimeOptions = {
  workspaceID: string;
  onEvent: (event: RealtimeEvent) => void | Promise<void>;
  onStatusChange?: (connected: boolean) => void;
  reconnectDelayMs?: number;
};

export type RealtimeConnection = {
  readonly connected: boolean;
  close(): void;
};

const cursorKey = (workspaceID: string) => `clickclack:${workspaceID}:cursor`;

export function connectRealtime(options: RealtimeOptions): RealtimeConnection {
  const { workspaceID, onEvent, onStatusChange } = options;
  const reconnectDelayMs = options.reconnectDelayMs ?? 1200;

  let socket: WebSocket | null = null;
  let reconnectTimer: number | undefined;
  let closed = false;
  let connected = false;
  let generation = 0;

  function setConnected(next: boolean) {
    if (connected === next) return;
    connected = next;
    onStatusChange?.(next);
  }

  function open() {
    if (closed) return;
    const currentGeneration = ++generation;
    const url = new URL("/api/realtime/ws", window.location.href);
    url.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    url.searchParams.set("workspace_id", workspaceID);
    const lastCursor = readCursor(workspaceID);
    if (lastCursor) url.searchParams.set("after_cursor", lastCursor);

    const current = new WebSocket(url);
    let deliveryFailed = false;
    let deliveryQueue = Promise.resolve();
    socket = current;

    current.addEventListener("open", () => {
      if (socket === current) setConnected(true);
    });

    current.addEventListener("message", (message) => {
      let event: RealtimeEvent;
      try {
        event = JSON.parse(String(message.data)) as RealtimeEvent;
      } catch {
        return;
      }
      if (!isRealtimeEvent(event)) return;
      deliveryQueue = deliveryQueue.then(async () => {
        if (closed || deliveryFailed || generation !== currentGeneration) return;
        try {
          await onEvent(event);
          if (event.cursor && generation === currentGeneration) {
            writeCursor(workspaceID, event.cursor);
          }
        } catch (error) {
          deliveryFailed = true;
          console.error("realtime event delivery failed", error);
          if (socket === current) current.close();
        }
      });
    });

    current.addEventListener("close", () => {
      if (socket !== current || closed) return;
      socket = null;
      generation++;
      setConnected(false);
      reconnectTimer = window.setTimeout(open, reconnectDelayMs);
    });
  }

  open();

  return {
    get connected() {
      return connected;
    },
    close() {
      closed = true;
      generation++;
      setConnected(false);
      if (reconnectTimer) window.clearTimeout(reconnectTimer);
      socket?.close();
      socket = null;
    },
  };
}

function readCursor(workspaceID: string): string {
  try {
    return window.localStorage.getItem(cursorKey(workspaceID)) || "";
  } catch {
    return "";
  }
}

function writeCursor(workspaceID: string, cursor: string) {
  try {
    window.localStorage.setItem(cursorKey(workspaceID), cursor);
  } catch {
    // Cursor persistence is an optimization; delivery must continue without it.
  }
}

function isRealtimeEvent(value: unknown): value is RealtimeEvent {
  return (
    typeof value === "object" && value !== null && typeof (value as RealtimeEvent).type === "string"
  );
}
