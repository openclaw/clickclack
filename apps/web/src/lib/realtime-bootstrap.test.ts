import assert from "node:assert/strict";
import test from "node:test";
import { prepareRealtimeCursor, requiresRealtimeResync } from "./realtime-bootstrap.ts";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

test("recognizes the server resync close code", () => {
  assert.equal(requiresRealtimeResync(4001), true);
  assert.equal(requiresRealtimeResync(1013), false);
});

test("uses an existing cursor without bootstrapping a fresh client", async () => {
  let fetches = 0;
  const result = await prepareRealtimeCursor({
    readCursor: () => "stored-cursor",
    async fetchTailCursor() {
      fetches += 1;
      return "tail-cursor";
    },
    isActive: () => true,
  });

  assert.deepEqual(result, {
    active: true,
    cursor: "stored-cursor",
    persistAfterOpen: false,
  });
  assert.equal(fetches, 0);
});

test("captures a fresh tail as a pending post-open checkpoint", async () => {
  const calls: string[] = [];
  const result = await prepareRealtimeCursor({
    readCursor() {
      calls.push("read");
      return null;
    },
    async fetchTailCursor() {
      calls.push("fetch");
      return "tail-cursor";
    },
    isActive: () => true,
  });

  assert.deepEqual(result, {
    active: true,
    cursor: "tail-cursor",
    persistAfterOpen: true,
  });
  assert.deepEqual(calls, ["read", "fetch"]);
});

test("authoritatively bootstraps a stored empty cursor", async () => {
  let fetches = 0;
  const result = await prepareRealtimeCursor({
    readCursor: () => "",
    async fetchTailCursor() {
      fetches += 1;
      return "tail-cursor";
    },
    isActive: () => true,
  });

  assert.deepEqual(result, {
    active: true,
    cursor: "tail-cursor",
    persistAfterOpen: true,
  });
  assert.equal(fetches, 1);
});

test("does not return a checkpoint after the bootstrap attempt becomes stale", async () => {
  const tail = deferred<string>();
  let active = true;
  const resultPromise = prepareRealtimeCursor({
    readCursor: () => null,
    fetchTailCursor: () => tail.promise,
    isActive: () => active,
  });

  active = false;
  tail.resolve("tail-cursor");
  assert.deepEqual(await resultPromise, { active: false });
});

test("propagates storage reads and bootstrap failures for reporting and retry", async () => {
  const readError = new Error("cursor read failed");
  await assert.rejects(
    prepareRealtimeCursor({
      readCursor() {
        throw readError;
      },
      async fetchTailCursor() {
        return "tail-cursor";
      },
      isActive: () => true,
    }),
    readError,
  );

  const fetchError = new Error("tail request failed");
  await assert.rejects(
    prepareRealtimeCursor({
      readCursor: () => null,
      async fetchTailCursor() {
        throw fetchError;
      },
      isActive: () => true,
    }),
    fetchError,
  );
});
