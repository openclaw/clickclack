import assert from "node:assert/strict";
import { test } from "node:test";
import {
  channelOrderStorageKey,
  parseChannelOrder,
  receiveChannelOrder,
  resolveChannelOrder,
  saveChannelOrder,
} from "./channel-order.ts";
import type { User } from "./types.ts";

test("account order wins, clears cached order, and preserves an oversized local tail", (t) => {
  const values = new Map<string, string>();
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      localStorage: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
      },
    },
  });
  t.after(() => {
    if (original) Object.defineProperty(globalThis, "window", original);
    else Reflect.deleteProperty(globalThis, "window");
  });
  const user = {
    id: "cache",
    sidebar_preferences: { channel_order: { workspace: ["b", "a"] } },
  } as unknown as User;
  values.set(channelOrderStorageKey("workspace", user.id), '["a","b","tail"]');
  assert.deepEqual(resolveChannelOrder(user, "workspace"), ["b", "a", "tail"]);
  user.sidebar_preferences!.channel_order!.workspace = [];
  assert.deepEqual(resolveChannelOrder(user, "workspace"), []);
  receiveChannelOrder(channelOrderStorageKey("workspace", user.id), '["new"]', user.id);
  assert.deepEqual(resolveChannelOrder(user, "workspace"), ["new"]);
  assert.deepEqual(parseChannelOrder('{"bad":true}'), []);
});

test("writes serialize and coalesce after failure; blocked storage keeps the session order", async () => {
  const user = { id: "queue" } as User;
  let finish!: () => void;
  const bodies: string[] = [];
  const request = async (_path: string, init: RequestInit) => {
    bodies.push(String(init.body));
    if (bodies.length === 1) {
      await new Promise<void>((resolve) => {
        finish = resolve;
      });
      throw new Error("offline");
    }
  };
  const first = saveChannelOrder(user.id, "workspace", ["a"], request, () => true);
  await Promise.resolve();
  const middle = saveChannelOrder(user.id, "workspace", ["b"], request, () => true);
  const last = saveChannelOrder(user.id, "workspace", ["c"], request, () => true);
  assert.equal(bodies.length, 1);
  finish();
  await Promise.all([first, middle, last]);
  assert.equal(bodies.length, 2);
  assert.deepEqual(JSON.parse(bodies[1]).sidebar_preferences.channel_order.workspace, ["c"]);
  assert.deepEqual(resolveChannelOrder(user, "workspace"), ["c"]);
  await saveChannelOrder(user.id, "workspace", ["d"], request, () => false);
  assert.equal(bodies.length, 2, "queued writes stop after an account change");
});
