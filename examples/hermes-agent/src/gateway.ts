import type { RealtimeEvent, RealtimeEventPage } from "@clickclack/sdk-ts";

import type { ConnectorLogger } from "./connector.ts";

const EVENT_PAGE_LIMIT = 500;
export const MAX_SOCKET_READ_AHEAD = 32;

export type ScheduledGatewayEvent = { completion: Promise<void> };

export class GatewayProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "GatewayProtocolError";
  }
}

export type GatewayEventClient = {
  events: {
    list(input: {
      workspaceId: string;
      afterCursor?: string;
      limit?: number;
      includeTail?: boolean;
    }): Promise<RealtimeEventPage>;
  };
};

export type GatewayRuntimeClient = GatewayEventClient & {
  events: GatewayEventClient["events"] & {
    subscribe(options: {
      workspaceId: string;
      afterCursor?: string;
      onEvent(event: RealtimeEvent): void;
      onClose?(): void;
    }): WebSocket;
  };
};

export async function captureInitialCursor(
  client: GatewayEventClient,
  workspaceId: string,
): Promise<string> {
  const page = await client.events.list({
    workspaceId,
    limit: EVENT_PAGE_LIMIT,
    includeTail: true,
  });
  if (page.tailCursor === undefined) {
    throw new GatewayProtocolError(
      "ClickClack server did not return an atomic tail cursor; upgrade the server before running this connector",
    );
  }
  return page.tailCursor;
}

export function nextLiveCursor(currentCursor: string, event: RealtimeEvent): string {
  if (!event.cursor) return currentCursor;
  if (event.cursor === currentCursor) {
    throw new GatewayProtocolError(
      `ClickClack realtime cursor did not advance from ${currentCursor}`,
    );
  }
  return event.cursor;
}

export async function drainEventBacklog(options: {
  client: GatewayEventClient;
  workspaceId: string;
  afterCursor: string;
  signal: AbortSignal;
  onEvent(event: RealtimeEvent): Promise<ScheduledGatewayEvent>;
}): Promise<string> {
  let afterCursor = options.afterCursor;
  while (!options.signal.aborted) {
    const page = await options.client.events.list({
      workspaceId: options.workspaceId,
      afterCursor,
      limit: EVENT_PAGE_LIMIT,
    });
    if (page.events.length === 0) return afterCursor;
    for (const event of page.events) {
      if (options.signal.aborted) return afterCursor;
      if (!event.cursor || event.cursor === afterCursor) {
        throw new GatewayProtocolError("ClickClack event backlog returned a non-advancing cursor");
      }
      afterCursor = event.cursor;
      const scheduled = await options.onEvent(event);
      await scheduled.completion;
    }
  }
  return afterCursor;
}

export async function runEventGateway(options: {
  client: GatewayRuntimeClient;
  workspaceId: string;
  signal: AbortSignal;
  reconnectMs: number;
  onEvent(event: RealtimeEvent): Promise<ScheduledGatewayEvent>;
  logger: ConnectorLogger;
}): Promise<void> {
  let initialized = false;
  let afterCursor = "";

  while (!options.signal.aborted) {
    try {
      if (!initialized) {
        afterCursor = await captureInitialCursor(options.client, options.workspaceId);
        initialized = true;
        options.logger.info("ClickClack initial event tail captured");
      } else {
        afterCursor = await drainEventBacklog({
          client: options.client,
          workspaceId: options.workspaceId,
          afterCursor,
          signal: options.signal,
          onEvent: options.onEvent,
        });
      }
      if (options.signal.aborted) return;
      afterCursor = await runSocketCycle({ ...options, afterCursor });
    } catch (error) {
      if (options.signal.aborted) return;
      if (error instanceof GatewayProtocolError) throw error;
      options.logger.warn("ClickClack realtime cycle failed; reconnecting", error);
    }
    if (!options.signal.aborted) await sleep(options.reconnectMs, options.signal);
  }
}

export async function runSocketCycle(options: {
  client: GatewayRuntimeClient;
  workspaceId: string;
  afterCursor: string;
  signal: AbortSignal;
  onEvent(event: RealtimeEvent): Promise<ScheduledGatewayEvent>;
  logger: ConnectorLogger;
}): Promise<string> {
  let afterCursor = options.afterCursor;
  let receivedCursor = options.afterCursor;
  let admissionQueue: Promise<void> = Promise.resolve();
  let commitQueue: Promise<void> = Promise.resolve();
  let pending = 0;
  let failure: unknown;
  let socket: WebSocket | undefined;

  return await new Promise<string>((resolve, reject) => {
    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      options.signal.removeEventListener("abort", abort);
      void admissionQueue
        .then(() => commitQueue)
        .then(() => (failure ? reject(failure) : resolve(afterCursor)), reject);
    };
    const abort = () => {
      socket?.close();
      finish();
    };
    options.signal.addEventListener("abort", abort, { once: true });

    socket = options.client.events.subscribe({
      workspaceId: options.workspaceId,
      ...(afterCursor ? { afterCursor } : {}),
      onEvent: (event) => {
        if (settled || failure || options.signal.aborted || !event.cursor) return;
        if (pending >= MAX_SOCKET_READ_AHEAD) {
          options.logger.info(
            "ClickClack read-ahead limit reached; reconnecting from committed cursor",
          );
          socket?.close();
          return;
        }

        let eventCursor: string;
        try {
          eventCursor = nextLiveCursor(receivedCursor, event);
          receivedCursor = eventCursor;
        } catch (error) {
          failure = error;
          socket?.close();
          return;
        }

        pending += 1;
        const admitted = admissionQueue.then(() => options.onEvent(event));
        admissionQueue = admitted.then(() => undefined);
        const completed = admitted.then((scheduled) => scheduled.completion);
        commitQueue = commitQueue
          .then(() => completed)
          .then(() => {
            afterCursor = eventCursor;
          })
          .finally(() => {
            pending -= 1;
          });
        void commitQueue.catch((error) => {
          failure = error;
          socket?.close();
        });
      },
      onClose: finish,
    });
    socket.addEventListener("error", () => {
      options.logger.warn("ClickClack websocket reported an error");
      socket?.close();
    });
    if (options.signal.aborted) abort();
  });
}

async function sleep(milliseconds: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return;
  await new Promise<void>((resolve) => {
    const timer = setTimeout(done, milliseconds);
    function done() {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    }
    signal.addEventListener("abort", done, { once: true });
  });
}
