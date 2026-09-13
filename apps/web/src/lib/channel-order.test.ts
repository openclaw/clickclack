import assert from "node:assert/strict";
import { test, type TestContext } from "node:test";
import {
  CHANNEL_ORDER_PATCH_DEBOUNCE_MS,
  channelOrderPatchBody,
  channelOrderStorageKey,
  channelOrderWorkspaceFromStorageKey,
  flushChannelOrderPatches,
  loadChannelOrder,
  markChannelOrderLocallyNewer,
  MAX_CHANNEL_ID_LENGTH,
  MAX_ROAMING_CHANNEL_ORDER_IDS,
  mergeChannelOrder,
  parseChannelOrder,
  resolveChannelOrder,
  storeChannelOrder,
} from "./channel-order.ts";
import type { User } from "./types.ts";

function storageState(t: TestContext, initial: Record<string, string> = {}) {
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  const values = new Map(Object.entries(initial));
  const writes = { count: 0 };
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      localStorage: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => {
          writes.count += 1;
          values.set(key, value);
        },
        removeItem: (key: string) => {
          writes.count += 1;
          values.delete(key);
        },
      },
    },
  });
  t.after(() => {
    if (original) Object.defineProperty(globalThis, "window", original);
    else Reflect.deleteProperty(globalThis, "window");
  });
  return { values, writes };
}

function storage(t: TestContext, initial: Record<string, string> = {}) {
  return storageState(t, initial).values;
}

type RecordedRequest = { path: string; init: RequestInit };

// The module never imports a transport, so a test hands it this one.
function recorder() {
  const requests: RecordedRequest[] = [];
  const request = (path: string, init: RequestInit) => {
    requests.push({ path, init });
    return Promise.resolve({});
  };
  return { requests, request };
}

// deferredRecorder hands back a transport whose requests stay unresolved until
// the test settles them, which is how an in-flight account write is held open.
function deferredRecorder(t?: TestContext) {
  const requests: RecordedRequest[] = [];
  const settlers: { resolve: () => void; reject: (error: Error) => void }[] = [];
  if (t) {
    const warn = console.warn;
    console.warn = () => {};
    t.after(() => {
      console.warn = warn;
    });
  }
  const request = (path: string, init: RequestInit) => {
    requests.push({ path, init });
    return new Promise<unknown>((resolve, reject) => {
      settlers.push({ resolve: () => resolve({}), reject });
    });
  };
  return { requests, request, settlers };
}

// Settling a request hands the module its continuation through microtasks, and
// a queued send starts in one of them. A macrotask turn lets all of that run.
function tick() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function rejectingRequest(t: TestContext) {
  const warn = console.warn;
  console.warn = () => {};
  t.after(() => {
    console.warn = warn;
  });
  return () => Promise.reject(new Error("offline"));
}

// Other tests in this file leave their own debounced writes in flight, so each
// assertion looks only at the workspace it just reordered.
function requestsFor(requests: RecordedRequest[], workspaceID: string) {
  return requests.filter((request) => String(request.init.body).includes(`"${workspaceID}"`));
}

function afterDebounce() {
  return new Promise((resolve) => setTimeout(resolve, CHANNEL_ORDER_PATCH_DEBOUNCE_MS + 150));
}

function user(id: string, channelOrder?: Record<string, string[]>): User {
  return {
    id,
    kind: "human",
    display_name: "Order Tester",
    handle: "order",
    avatar_url: "",
    created_at: "2026-09-13T00:00:00Z",
    ...(channelOrder ? { sidebar_preferences: { channel_order: channelOrder } } : {}),
  };
}

test("an account order replaces the local cache and is written back into it", (t) => {
  const values = storage(t, {
    [channelOrderStorageKey("wsp_1", "usr_1")]: JSON.stringify(["chn_a", "chn_b"]),
  });
  const resolved = resolveChannelOrder(user("usr_1", { wsp_1: ["chn_b", "chn_a"] }), "wsp_1");
  assert.deepEqual(resolved, ["chn_b", "chn_a"]);
  assert.deepEqual(loadChannelOrder("wsp_1", "usr_1"), ["chn_b", "chn_a"]);
  assert.equal(
    values.get(channelOrderStorageKey("wsp_1", "usr_1")),
    JSON.stringify(["chn_b", "chn_a"]),
  );
});

test("no account order leaves the cache alone and keeps serving it", (t) => {
  const values = storage(t, {
    [channelOrderStorageKey("wsp_2", "usr_2")]: JSON.stringify(["chn_a", "chn_b"]),
  });
  const resolved = resolveChannelOrder(user("usr_2"), "wsp_2");
  assert.deepEqual(resolved, ["chn_a", "chn_b"]);
  assert.equal(
    values.get(channelOrderStorageKey("wsp_2", "usr_2")),
    JSON.stringify(["chn_a", "chn_b"]),
  );
});

test("an account order for another workspace never touches this one", (t) => {
  const values = storage(t);
  const resolved = resolveChannelOrder(user("usr_3", { wsp_other: ["chn_a"] }), "wsp_3");
  assert.deepEqual(resolved, []);
  assert.equal(values.has(channelOrderStorageKey("wsp_3", "usr_3")), false);
});

test("a cleared account order clears the cache", (t) => {
  const values = storage(t, {
    [channelOrderStorageKey("wsp_4", "usr_4")]: JSON.stringify(["chn_a"]),
  });
  assert.deepEqual(resolveChannelOrder(user("usr_4", { wsp_4: [] }), "wsp_4"), []);
  assert.equal(values.has(channelOrderStorageKey("wsp_4", "usr_4")), false);
});

test("a reorder made in this session outlives a stale account snapshot", async (t) => {
  storage(t);
  const { request } = recorder();
  storeChannelOrder("wsp_5", "usr_5", ["chn_c", "chn_a"], request);
  const resolved = resolveChannelOrder(user("usr_5", { wsp_5: ["chn_a", "chn_c"] }), "wsp_5");
  assert.deepEqual(resolved, ["chn_c", "chn_a"]);
  await afterDebounce();
});

test("unavailable storage still resolves and still accepts a reorder", async (t) => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      localStorage: {
        getItem() {
          throw new Error("blocked storage");
        },
        setItem() {
          throw new Error("blocked storage");
        },
        removeItem() {
          throw new Error("blocked storage");
        },
      },
    },
  });
  t.after(() => {
    if (original) Object.defineProperty(globalThis, "window", original);
    else Reflect.deleteProperty(globalThis, "window");
  });
  const { request } = recorder();
  assert.deepEqual(resolveChannelOrder(user("usr_6", { wsp_6: ["chn_a"] }), "wsp_6"), ["chn_a"]);
  assert.doesNotThrow(() => storeChannelOrder("wsp_6", "usr_6", ["chn_a"], request));
  await afterDebounce();
});

test("a reorder writes the cache first and patches the account once after the debounce", async (t) => {
  const values = storage(t);
  const { requests, request } = recorder();
  storeChannelOrder("wsp_8", "usr_8", ["chn_a"], request);
  storeChannelOrder("wsp_8", "usr_8", ["chn_b", "chn_a"], request);
  assert.equal(
    values.get(channelOrderStorageKey("wsp_8", "usr_8")),
    JSON.stringify(["chn_b", "chn_a"]),
  );
  assert.equal(requestsFor(requests, "wsp_8").length, 0);
  await afterDebounce();
  const sent = requestsFor(requests, "wsp_8");
  assert.equal(sent.length, 1);
  assert.equal(sent[0].path, "/api/me");
  assert.equal(sent[0].init.method, "PATCH");
  assert.equal(sent[0].init.keepalive, undefined);
  assert.deepEqual(JSON.parse(String(sent[0].init.body)), {
    sidebar_preferences: { channel_order: { wsp_8: ["chn_b", "chn_a"] } },
  });
});

test("a flush sends the latest pending order once, with keepalive", async (t) => {
  storage(t);
  const { requests, request } = recorder();
  storeChannelOrder("wsp_10", "usr_10", ["chn_a"], request);
  storeChannelOrder("wsp_10", "usr_10", ["chn_b", "chn_a"], request);
  assert.equal(requestsFor(requests, "wsp_10").length, 0);

  flushChannelOrderPatches(request);
  const sent = requestsFor(requests, "wsp_10");
  assert.equal(sent.length, 1);
  assert.equal(sent[0].path, "/api/me");
  assert.equal(sent[0].init.method, "PATCH");
  assert.equal(sent[0].init.keepalive, true);
  assert.deepEqual(JSON.parse(String(sent[0].init.body)), {
    sidebar_preferences: { channel_order: { wsp_10: ["chn_b", "chn_a"] } },
  });

  // The flushed timer is gone, so the debounce cannot fire it a second time.
  await afterDebounce();
  assert.equal(requestsFor(requests, "wsp_10").length, 1);

  // A second flush with nothing pending sends nothing.
  flushChannelOrderPatches(request);
  assert.equal(requestsFor(requests, "wsp_10").length, 1);

  // A later reorder starts a fresh debounce.
  storeChannelOrder("wsp_10", "usr_10", ["chn_a", "chn_b"], request);
  assert.equal(requestsFor(requests, "wsp_10").length, 1);
  await afterDebounce();
  const resent = requestsFor(requests, "wsp_10");
  assert.equal(resent.length, 2);
  assert.equal(resent[1].init.keepalive, undefined);
  assert.deepEqual(JSON.parse(String(resent[1].init.body)), {
    sidebar_preferences: { channel_order: { wsp_10: ["chn_a", "chn_b"] } },
  });
});

test("a failed account patch leaves the local order in place", async (t) => {
  const values = storage(t);
  storeChannelOrder("wsp_9", "usr_9", ["chn_b", "chn_a"], rejectingRequest(t));
  await afterDebounce();
  assert.equal(
    values.get(channelOrderStorageKey("wsp_9", "usr_9")),
    JSON.stringify(["chn_b", "chn_a"]),
  );
  assert.deepEqual(loadChannelOrder("wsp_9", "usr_9"), ["chn_b", "chn_a"]);
});

test("an account order is sanitized the same way the cache is", () => {
  const tooLong = "x".repeat(MAX_CHANNEL_ID_LENGTH + 1);
  assert.deepEqual(mergeChannelOrder(["chn_a", "chn_a", "", tooLong, "chn_b"], []), [
    "chn_a",
    "chn_b",
  ]);
  assert.deepEqual(mergeChannelOrder(undefined, ["chn_a"]), ["chn_a"]);
  assert.deepEqual(mergeChannelOrder([], ["chn_a"]), []);
});

test("parseChannelOrder keeps its existing rules", () => {
  assert.deepEqual(parseChannelOrder(null), []);
  assert.deepEqual(parseChannelOrder("not-json"), []);
  assert.deepEqual(parseChannelOrder(JSON.stringify(["chn_a", "chn_a"])), ["chn_a"]);
  assert.deepEqual(parseChannelOrder(JSON.stringify(["chn_a", 7])), []);
  assert.deepEqual(
    parseChannelOrder(JSON.stringify(["chn_a", "x".repeat(MAX_CHANNEL_ID_LENGTH + 1)])),
    [],
  );
});

test("the patch body carries one workspace and stops at the roaming cap", () => {
  const order = Array.from({ length: MAX_ROAMING_CHANNEL_ORDER_IDS + 5 }, (_, i) => `chn_${i}`);
  const body = channelOrderPatchBody("wsp_7", order);
  assert.deepEqual(Object.keys(body.sidebar_preferences.channel_order), ["wsp_7"]);
  assert.equal(body.sidebar_preferences.channel_order.wsp_7.length, MAX_ROAMING_CHANNEL_ORDER_IDS);
  assert.equal(body.sidebar_preferences.channel_order.wsp_7[0], "chn_0");
});

test("an order that arrives during an account write waits for it and lands last", async (t) => {
  storage(t);
  const { requests, request, settlers } = deferredRecorder();
  storeChannelOrder("wsp_20", "usr_20", ["chn_a"], request);
  await afterDebounce();
  assert.equal(requestsFor(requests, "wsp_20").length, 1);

  // The first request is still unresolved, so the second order cannot be sent
  // yet: two writes in flight at once can land in either order.
  storeChannelOrder("wsp_20", "usr_20", ["chn_b", "chn_a"], request);
  await afterDebounce();
  assert.equal(requestsFor(requests, "wsp_20").length, 1);

  settlers[0].resolve();
  await tick();
  const sent = requestsFor(requests, "wsp_20");
  assert.equal(sent.length, 2);
  assert.deepEqual(JSON.parse(String(sent[1].init.body)), {
    sidebar_preferences: { channel_order: { wsp_20: ["chn_b", "chn_a"] } },
  });
  settlers[1].resolve();
  await tick();
});

// The coalescing rule: the in-flight request is never cancelled, and only the
// newest order waiting behind it is sent once it settles. Three reorders during
// one in-flight write therefore make two requests, not three.
test("only the newest order waiting on an account write is sent", async (t) => {
  storage(t);
  const { requests, request, settlers } = deferredRecorder();
  storeChannelOrder("wsp_26", "usr_26", ["chn_a"], request);
  await afterDebounce();
  storeChannelOrder("wsp_26", "usr_26", ["chn_b", "chn_a"], request);
  await afterDebounce();
  storeChannelOrder("wsp_26", "usr_26", ["chn_c", "chn_b", "chn_a"], request);
  await afterDebounce();
  assert.equal(requestsFor(requests, "wsp_26").length, 1);

  settlers[0].resolve();
  await tick();
  const sent = requestsFor(requests, "wsp_26");
  assert.equal(sent.length, 2);
  assert.deepEqual(JSON.parse(String(sent[1].init.body)), {
    sidebar_preferences: { channel_order: { wsp_26: ["chn_c", "chn_b", "chn_a"] } },
  });

  settlers[1].resolve();
  await tick();
  assert.equal(requestsFor(requests, "wsp_26").length, 2);
});

test("a flush during an account write joins the queue instead of racing it", async (t) => {
  storage(t);
  const { requests, request, settlers } = deferredRecorder();
  storeChannelOrder("wsp_21", "usr_21", ["chn_a"], request);
  await afterDebounce();
  assert.equal(requestsFor(requests, "wsp_21").length, 1);

  storeChannelOrder("wsp_21", "usr_21", ["chn_b", "chn_a"], request);
  flushChannelOrderPatches(request);
  assert.equal(requestsFor(requests, "wsp_21").length, 1);

  settlers[0].resolve();
  await tick();
  const sent = requestsFor(requests, "wsp_21");
  assert.equal(sent.length, 2);
  assert.equal(sent[1].init.keepalive, true);
  assert.deepEqual(JSON.parse(String(sent[1].init.body)), {
    sidebar_preferences: { channel_order: { wsp_21: ["chn_b", "chn_a"] } },
  });
  settlers[1].resolve();
  await tick();
});

test("a failed account write does not block the next order", async (t) => {
  storage(t);
  const { requests, request, settlers } = deferredRecorder(t);
  storeChannelOrder("wsp_22", "usr_22", ["chn_a"], request);
  await afterDebounce();
  storeChannelOrder("wsp_22", "usr_22", ["chn_b", "chn_a"], request);
  await afterDebounce();
  assert.equal(requestsFor(requests, "wsp_22").length, 1);

  settlers[0].reject(new Error("offline"));
  await tick();
  const sent = requestsFor(requests, "wsp_22");
  assert.equal(sent.length, 2);
  assert.deepEqual(JSON.parse(String(sent[1].init.body)), {
    sidebar_preferences: { channel_order: { wsp_22: ["chn_b", "chn_a"] } },
  });
  settlers[1].resolve();
  await tick();
});

test("an account snapshot applies once per profile and a newer cache wins after that", (t) => {
  const key = channelOrderStorageKey("wsp_23", "usr_23");
  const { values, writes } = storageState(t, { [key]: JSON.stringify(["chn_a", "chn_b"]) });
  const profile = user("usr_23", { wsp_23: ["chn_b", "chn_a"] });

  assert.deepEqual(resolveChannelOrder(profile, "wsp_23"), ["chn_b", "chn_a"]);
  const writesAfterApply = writes.count;

  // Another tab of this browser saved a newer order into the shared cache.
  values.set(key, JSON.stringify(["chn_a", "chn_b"]));

  // Leaving the workspace and coming back re-resolves from that cache. The
  // boot-time snapshot on the same profile object cannot replay over it, and
  // nothing is written back.
  assert.deepEqual(resolveChannelOrder(profile, "wsp_23"), ["chn_a", "chn_b"]);
  assert.equal(writes.count, writesAfterApply);
  assert.equal(values.get(key), JSON.stringify(["chn_a", "chn_b"]));

  // A fresh /api/me is a new profile object and applies again.
  assert.deepEqual(resolveChannelOrder(user("usr_23", { wsp_23: ["chn_b", "chn_a"] }), "wsp_23"), [
    "chn_b",
    "chn_a",
  ]);
});

test("a cache write from another tab marks that workspace locally newer", (t) => {
  const key = channelOrderStorageKey("wsp_24", "usr_24");
  const { writes } = storageState(t, { [key]: JSON.stringify(["chn_a"]) });

  assert.equal(channelOrderWorkspaceFromStorageKey(key, "usr_24"), "wsp_24");
  assert.equal(
    channelOrderWorkspaceFromStorageKey(channelOrderStorageKey("wsp_24", "usr_other"), "usr_24"),
    null,
  );
  assert.equal(
    channelOrderWorkspaceFromStorageKey("clickclack:sidebar-sections:wsp_24", "usr_24"),
    null,
  );
  assert.equal(channelOrderWorkspaceFromStorageKey(null, "usr_24"), null);

  markChannelOrderLocallyNewer("wsp_24", "usr_24");
  const before = writes.count;
  assert.deepEqual(resolveChannelOrder(user("usr_24", { wsp_24: ["chn_z"] }), "wsp_24"), ["chn_a"]);
  assert.equal(writes.count, before);
});

test("local positions past the roaming cap survive an account order", (t) => {
  const local = Array.from({ length: 600 }, (_, index) => `chn_${index}`);
  // The account only ever sees the leading window, reordered here so the
  // account copy is visibly in charge of the part it covers.
  const window = local.slice(0, MAX_ROAMING_CHANNEL_ORDER_IDS);
  const account = [window[window.length - 1], ...window.slice(0, window.length - 1)];
  const key = channelOrderStorageKey("wsp_25", "usr_25");
  const { values } = storageState(t, { [key]: JSON.stringify(local) });

  const resolved = resolveChannelOrder(user("usr_25", { wsp_25: account }), "wsp_25");
  assert.equal(resolved.length, local.length);
  assert.deepEqual(resolved.slice(0, MAX_ROAMING_CHANNEL_ORDER_IDS), account);
  assert.deepEqual(
    resolved.slice(MAX_ROAMING_CHANNEL_ORDER_IDS),
    local.slice(MAX_ROAMING_CHANNEL_ORDER_IDS),
  );
  assert.equal(values.get(key), JSON.stringify(resolved));
});

test("an account order leads the local order and a cleared one wipes it", () => {
  assert.deepEqual(mergeChannelOrder(["chn_b"], ["chn_a", "chn_b", "chn_c"]), [
    "chn_b",
    "chn_a",
    "chn_c",
  ]);
  // A clear is an explicit "no custom order", so no local tail is carried over.
  assert.deepEqual(mergeChannelOrder([], ["chn_a", "chn_b"]), []);
});
