import assert from "node:assert/strict";
import { test, type TestContext } from "node:test";
import { loadActivityPreferences, storeActivityPreference } from "./chat/activity-preferences.ts";

function storage(t: TestContext, initial: Record<string, string>) {
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  const values = new Map(Object.entries(initial));
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
  return values;
}

test("explicit visibility choices survive loading an older hide-all preference", (t) => {
  storage(t, { "clickclack:show-agent-activity:v1": "0" });
  assert.equal(loadActivityPreferences().hideCommentary, true);
  assert.equal(loadActivityPreferences().hideToolCalls, true);
  storeActivityPreference("hideCommentary", false);
  assert.equal(loadActivityPreferences().hideCommentary, false);
  assert.equal(loadActivityPreferences().hideToolCalls, true);
  storeActivityPreference("hideToolCalls", false);
  assert.equal(loadActivityPreferences().hideToolCalls, false);
});

test("older activity choices remain defaults when no individual choice is stored", (t) => {
  storage(t, {
    "clickclack:show-agent-activity:v1": "0",
    "clickclack:hide-commentary:v1": "invalid",
    "clickclack:user-align:v1": "right",
  });
  assert.deepEqual(loadActivityPreferences(), {
    hideCommentary: true,
    hideToolCalls: true,
    userAlign: "right",
    otherAlign: "left",
  });
});

test("an explicit hidden choice remains hidden without the older preference", (t) => {
  storage(t, { "clickclack:hide-tool-calls:v1": "1" });
  assert.equal(loadActivityPreferences().hideCommentary, false);
  assert.equal(loadActivityPreferences().hideToolCalls, true);
});
