import test from "node:test";
import assert from "node:assert/strict";
import {
  channelDeletedNotice,
  channelDeletionBlockerMessage,
  channelDeletionConfirmed,
} from "./channel-deletion.ts";

test("channelDeletionConfirmed requires the exact routing name", () => {
  const channel = { name: "ventas-asia" };
  assert.equal(channelDeletionConfirmed("ventas-asia", channel), true);
  assert.equal(channelDeletionConfirmed("  ventas-asia  ", channel), true);
  assert.equal(channelDeletionConfirmed("ventas-as", channel), false);
  assert.equal(channelDeletionConfirmed("#ventas-asia", channel), false);
  assert.equal(channelDeletionConfirmed("Ventas-Asia", channel), false);
  assert.equal(channelDeletionConfirmed("", channel), false);
});

test("channelDeletionBlockerMessage explains each server blocker", () => {
  assert.match(channelDeletionBlockerMessage({ blocker: "last_channel" }), /at least one channel/);
  assert.match(
    channelDeletionBlockerMessage({ blocker: "provisioned_channel" }),
    /recreates this channel/,
  );
  assert.equal(channelDeletionBlockerMessage({}), "");
});

test("channelDeletedNotice names the actor when known", () => {
  assert.equal(
    channelDeletedNotice("ventas-asia", "Sergio"),
    "#ventas-asia was deleted by Sergio.",
  );
  assert.equal(channelDeletedNotice("ventas-asia", ""), "#ventas-asia was deleted.");
  assert.equal(channelDeletedNotice("ventas-asia", "Sergio", true), "Deleted #ventas-asia.");
});
