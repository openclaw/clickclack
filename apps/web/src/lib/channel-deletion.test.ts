import test from "node:test";
import assert from "node:assert/strict";
import { channelDeletionConfirmed } from "./channel-deletion.ts";

test("channelDeletionConfirmed requires the exact routing name", () => {
  const channel = { name: "ventas-asia" };
  assert.equal(channelDeletionConfirmed("ventas-asia", channel), true);
  assert.equal(channelDeletionConfirmed("  ventas-asia  ", channel), true);
  assert.equal(channelDeletionConfirmed("ventas-as", channel), false);
  assert.equal(channelDeletionConfirmed("#ventas-asia", channel), false);
  assert.equal(channelDeletionConfirmed("Ventas-Asia", channel), false);
  assert.equal(channelDeletionConfirmed("", channel), false);
});
