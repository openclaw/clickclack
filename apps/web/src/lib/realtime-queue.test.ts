import assert from "node:assert/strict";
import test from "node:test";
import { createRealtimeQueue } from "./realtime-queue.ts";

type TestEvent = {
  name: string;
  cursor?: string;
};

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

test("processes events in arrival order and checkpoints only after success", async () => {
  const first = deferred();
  const calls: string[] = [];
  const cursors: string[] = [];
  const queue = createRealtimeQueue<TestEvent>({
    async onEvent(event) {
      calls.push(`start:${event.name}`);
      if (event.name === "first") await first.promise;
      calls.push(`end:${event.name}`);
    },
    persistCursor(cursor) {
      cursors.push(cursor);
      calls.push(`checkpoint:${cursor}`);
    },
  });

  queue.enqueue({ name: "first", cursor: "cursor-1" });
  queue.enqueue({ name: "ephemeral" });
  queue.enqueue({ name: "last", cursor: "cursor-2" });
  await Promise.resolve();

  assert.deepEqual(calls, ["start:first"]);
  assert.deepEqual(cursors, []);

  first.resolve();
  await queue.whenIdle();

  assert.deepEqual(calls, [
    "start:first",
    "end:first",
    "checkpoint:cursor-1",
    "start:ephemeral",
    "end:ephemeral",
    "start:last",
    "end:last",
    "checkpoint:cursor-2",
  ]);
  assert.deepEqual(cursors, ["cursor-1", "cursor-2"]);
});

test("stops after a handler failure without checkpointing later events", async () => {
  const calls: string[] = [];
  const cursors: string[] = [];
  const errors: unknown[] = [];
  let failures = 0;
  const expected = new Error("handler failed");
  const queue = createRealtimeQueue<TestEvent>({
    onEvent(event) {
      calls.push(event.name);
      if (event.name === "broken") throw expected;
    },
    persistCursor(cursor) {
      cursors.push(cursor);
    },
    onError(error) {
      errors.push(error);
      throw new Error("reporter failed");
    },
    onFailure() {
      failures += 1;
      throw new Error("cleanup failed");
    },
  });

  queue.enqueue({ name: "first", cursor: "cursor-1" });
  queue.enqueue({ name: "broken", cursor: "cursor-2" });
  queue.enqueue({ name: "later", cursor: "cursor-3" });
  await queue.whenIdle();

  assert.deepEqual(calls, ["first", "broken"]);
  assert.deepEqual(cursors, ["cursor-1"]);
  assert.deepEqual(errors, [expected]);
  assert.equal(failures, 1);
});

test("drains a large replay backlog after a blocked handler", async () => {
  const first = deferred();
  const calls: string[] = [];
  const cursors: string[] = [];
  const queue = createRealtimeQueue<TestEvent>({
    async onEvent(event) {
      calls.push(event.name);
      if (event.name === "first") await first.promise;
    },
    persistCursor(cursor) {
      cursors.push(cursor);
    },
  });

  queue.enqueue({ name: "first", cursor: "cursor-0" });
  for (let index = 1; index <= 256; index += 1) {
    queue.enqueue({ name: `event-${index}`, cursor: `cursor-${index}` });
  }
  first.resolve();
  await queue.whenIdle();

  assert.equal(calls.length, 257);
  assert.equal(calls.at(-1), "event-256");
  assert.equal(cursors.at(-1), "cursor-256");
});

test("fails and clears a live backlog beyond the configured bound", async () => {
  const first = deferred();
  const calls: string[] = [];
  const errors: unknown[] = [];
  let failures = 0;
  const queue = createRealtimeQueue<TestEvent>({
    async onEvent(event) {
      calls.push(event.name);
      if (event.name === "first") await first.promise;
    },
    persistCursor() {},
    onError(error) {
      errors.push(error);
    },
    onFailure() {
      failures += 1;
    },
    maxPendingEvents: 2,
  });

  queue.enqueue({ name: "first" });
  queue.enqueue({ name: "second" });
  queue.enqueue({ name: "third" });
  queue.enqueue({ name: "overflow" });
  first.resolve();
  await queue.whenIdle();

  assert.deepEqual(calls, ["first"]);
  assert.equal(errors.length, 1);
  assert.match(String(errors[0]), /queue overflowed/);
  assert.equal(failures, 1);
});

test("ignores queued work after the connection becomes stale", async () => {
  const first = deferred();
  const calls: string[] = [];
  const cursors: string[] = [];
  let active = true;
  const queue = createRealtimeQueue<TestEvent>({
    async onEvent(event) {
      calls.push(event.name);
      await first.promise;
    },
    persistCursor(cursor) {
      cursors.push(cursor);
    },
    isActive: () => active,
  });

  queue.enqueue({ name: "first", cursor: "cursor-1" });
  queue.enqueue({ name: "later", cursor: "cursor-2" });
  await Promise.resolve();
  active = false;
  first.resolve();
  await queue.whenIdle();

  assert.deepEqual(calls, ["first"]);
  assert.deepEqual(cursors, []);
});
