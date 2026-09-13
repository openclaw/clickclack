import assert from "node:assert/strict";
import { test } from "node:test";
import { updateAgentProgress } from "./chat/agent-progress.ts";

test("status-only finalization keeps the existing progress content", () => {
  const first = updateAgentProgress(
    [],
    {
      user_id: "bot-a",
      turn_id: "turn",
      op: "append",
      line: { id: "tool", kind: "tool", tool_name: "Search", text: "Finding results" },
    },
    100,
  );
  const finished = updateAgentProgress(
    first,
    {
      user_id: "bot-a",
      turn_id: "turn",
      op: "finalize",
      line: { id: "tool", status: "done" },
    },
    200,
  );
  assert.equal(finished[0].lines[0].text, "Finding results");
  assert.equal(finished[0].lines[0].toolName, "Search");
  assert.equal(finished[0].lines[0].finalized, true);
  assert.equal(finished[0].lines[0].status, "done");
  assert.equal(first[0].lines[0].finalized, false);
});

test("clearing one sender keeps another sender's same-named turn", () => {
  const event = { turn_id: "turn", op: "append", line: { id: "line", text: "Working" } };
  const first = updateAgentProgress([], { ...event, user_id: "bot-a" }, 0);
  const both = updateAgentProgress(first, { ...event, user_id: "bot-b" }, 0);
  const remaining = updateAgentProgress(
    both,
    { user_id: "bot-a", turn_id: "turn", op: "clear" },
    1,
  );
  assert.deepEqual(
    remaining.map((turn) => turn.userId),
    ["bot-b"],
  );
});

test("empty new progress lines do not create a turn or refresh its lifetime", () => {
  const first = updateAgentProgress(
    [],
    {
      user_id: "bot",
      turn_id: "turn",
      op: "append",
      line: { id: "line", title: "Working" },
    },
    0,
  );
  const ignored = updateAgentProgress(
    first,
    {
      user_id: "bot",
      turn_id: "turn",
      op: "update",
      line: { id: "empty", status: "done" },
    },
    100,
  );
  assert.equal(ignored, first);
});
