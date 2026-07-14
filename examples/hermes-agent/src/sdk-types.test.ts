import assert from "node:assert/strict";
import test from "node:test";

import type { AgentProgressPayload, EphemeralEventInput } from "@clickclack/sdk-ts";

const payload: AgentProgressPayload = {
  turn_id: "turn_1",
  op: "append",
  line: { id: "lifecycle", kind: "lifecycle", status: "running" },
};

const channelTarget: EphemeralEventInput = {
  workspaceId: "wsp_1",
  channelId: "chn_1",
  type: "agent.progress",
  payload,
};

const directTarget: EphemeralEventInput = {
  workspaceId: "wsp_1",
  directConversationId: "dm_1",
  type: "agent.progress",
  payload,
};

// @ts-expect-error agent progress requires exactly one target
const missingTarget: EphemeralEventInput = {
  workspaceId: "wsp_1",
  type: "agent.progress",
  payload,
};

// @ts-expect-error agent progress cannot target a channel and DM simultaneously
const duplicateTarget: EphemeralEventInput = {
  workspaceId: "wsp_1",
  channelId: "chn_1",
  directConversationId: "dm_1",
  type: "agent.progress",
  payload,
};

test("ephemeral event target types accept channel or DM inputs", () => {
  assert.equal(channelTarget.channelId, "chn_1");
  assert.equal(directTarget.directConversationId, "dm_1");
  assert.equal(missingTarget.type, "agent.progress");
  assert.equal(duplicateTarget.type, "agent.progress");
});
