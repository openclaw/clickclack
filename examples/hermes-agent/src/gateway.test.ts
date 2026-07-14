import assert from "node:assert/strict";
import test from "node:test";

import type { RealtimeEvent, RealtimeEventPage } from "../../../packages/sdk-ts/src/index.ts";
import {
  GatewayProtocolError,
  MAX_SOCKET_READ_AHEAD,
  captureInitialCursor,
  drainEventBacklog,
  nextLiveCursor,
  runSocketCycle,
  type GatewayEventClient,
  type GatewayRuntimeClient,
} from "./gateway.ts";

function event(cursor: string): RealtimeEvent {
  return {
    id: `evt_${cursor}`,
    cursor,
    type: "message.created",
    workspace_id: "wsp_1",
    channel_id: "chn_1",
    created_at: "2026-07-14T00:00:00Z",
    payload: { message_id: `msg_${cursor}`, author_id: "usr_1" },
  };
}

function clientFromPages(pages: RealtimeEventPage[]): GatewayEventClient {
  return {
    events: {
      list: async () => pages.shift() ?? { events: [] },
    },
  };
}

test("captureInitialCursor prefers the atomically captured tail", async () => {
  const client = clientFromPages([{ events: [event("cur_old")], tailCursor: "cur_tail" }]);

  assert.equal(await captureInitialCursor(client, "wsp_1"), "cur_tail");
});

test("captureInitialCursor fails closed without an atomic tail", async () => {
  const client = clientFromPages([{ events: [event("cur_1"), event("cur_2")] }]);

  await assert.rejects(() => captureInitialCursor(client, "wsp_1"), /atomic tail cursor/);
});

test("live ephemeral events do not advance the durable cursor", () => {
  const ephemeral = event("");
  ephemeral.type = "agent.progress";

  assert.equal(nextLiveCursor("cur_1", ephemeral), "cur_1");
  assert.equal(nextLiveCursor("cur_1", event("cur_2")), "cur_2");
  assert.throws(() => nextLiveCursor("cur_2", event("cur_2")), GatewayProtocolError);
});

test("drainEventBacklog processes pages serially and returns the latest cursor", async () => {
  const client = clientFromPages([
    { events: [event("cur_2"), event("cur_3")] },
    { events: [event("cur_4")] },
    { events: [] },
  ]);
  const order: string[] = [];

  const cursor = await drainEventBacklog({
    client,
    workspaceId: "wsp_1",
    afterCursor: "cur_1",
    signal: new AbortController().signal,
    onEvent: async (value) => {
      await Promise.resolve();
      order.push(value.cursor);
      return { completion: Promise.resolve() };
    },
  });

  assert.equal(cursor, "cur_4");
  assert.deepEqual(order, ["cur_2", "cur_3", "cur_4"]);
});

test("drainEventBacklog advances before dispatch and rejects a non-advancing cursor", async () => {
  const client = clientFromPages([{ events: [event("cur_1")] }]);
  let dispatched = false;

  await assert.rejects(
    () =>
      drainEventBacklog({
        client,
        workspaceId: "wsp_1",
        afterCursor: "cur_1",
        signal: new AbortController().signal,
        onEvent: async () => {
          dispatched = true;
          return { completion: Promise.resolve() };
        },
      }),
    GatewayProtocolError,
  );
  assert.equal(dispatched, false);
});

test("drainEventBacklog stops cleanly when aborted", async () => {
  const abort = new AbortController();
  const client = clientFromPages([{ events: [event("cur_2"), event("cur_3")] }]);
  const order: string[] = [];

  const cursor = await drainEventBacklog({
    client,
    workspaceId: "wsp_1",
    afterCursor: "cur_1",
    signal: abort.signal,
    onEvent: async (value) => {
      order.push(value.cursor);
      abort.abort();
      return { completion: Promise.resolve() };
    },
  });

  assert.equal(cursor, "cur_2");
  assert.deepEqual(order, ["cur_2"]);
});

test("socket read-ahead closes at the bound and commits only completed events", async () => {
  let socket: TestSocket | undefined;
  const client: GatewayRuntimeClient = {
    events: {
      list: async () => ({ events: [] }),
      subscribe: (options) => {
        socket = new TestSocket(options.onClose);
        queueMicrotask(() => {
          for (let index = 1; index <= MAX_SOCKET_READ_AHEAD + 1; index += 1) {
            options.onEvent(event(`cur_${index}`));
          }
        });
        return socket as unknown as WebSocket;
      },
    },
  };
  let releaseFirst!: () => void;
  const firstCompletion = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  const admitted: string[] = [];
  const cycle = runSocketCycle({
    client,
    workspaceId: "wsp_1",
    afterCursor: "cur_0",
    signal: new AbortController().signal,
    onEvent: async (value) => {
      admitted.push(value.cursor);
      return {
        completion: value.cursor === "cur_1" ? firstCompletion : Promise.resolve(),
      };
    },
    logger: { info: () => {}, warn: () => {}, error: () => {} },
  });

  await waitForCondition(
    () => socket?.closed === true && admitted.length === MAX_SOCKET_READ_AHEAD,
  );
  assert.equal(admitted.length, MAX_SOCKET_READ_AHEAD);
  releaseFirst();
  assert.equal(await cycle, `cur_${MAX_SOCKET_READ_AHEAD}`);
});

class TestSocket extends EventTarget {
  closed = false;
  private readonly onClose?: () => void;

  constructor(onClose?: () => void) {
    super();
    this.onClose = onClose;
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.onClose?.();
  }
}

async function waitForCondition(predicate: () => boolean): Promise<void> {
  const deadline = Date.now() + 2_000;
  while (!predicate()) {
    if (Date.now() >= deadline) throw new Error("condition timed out");
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
}
