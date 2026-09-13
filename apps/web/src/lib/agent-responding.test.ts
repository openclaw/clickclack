import test from "node:test";
import assert from "node:assert/strict";
import { agentProgressTurnKey } from "./chat/agent-progress.ts";
import { agentNameFor, respondingAgentNames } from "./agent-responding.ts";
import type { User, WorkspaceBotCommand } from "./types";

const bot = (id: string, display_name: string, handle = "") =>
  ({
    id,
    handle,
    display_name,
    avatar_url: "",
  }) as WorkspaceBotCommand["bot"];

const command = (id: string, display_name: string, handle = ""): WorkspaceBotCommand =>
  ({
    id: `${id}-command`,
    command: "/test",
    description: "",
    args_hint: "",
    bot: bot(id, display_name, handle),
    created_at: "",
    updated_at: "",
  }) as WorkspaceBotCommand;

const user = (id: string, display_name: string, handle = ""): User =>
  ({ id, display_name, handle }) as User;

test("keys progress turns by user and turn ID", () => {
  assert.notEqual(agentProgressTurnKey("bot-a", "same"), agentProgressTurnKey("bot-b", "same"));
  assert.equal(agentProgressTurnKey("bot-a", "same"), agentProgressTurnKey("bot-a", "same"));
});

test("prefers loaded bot-command identity over transient message lookup", () => {
  const commands = [command("bot-a", "Blackbird", "blackbird")];
  assert.equal(
    agentNameFor("bot-a", commands, () => undefined),
    "Blackbird",
  );
});

test("falls back to a loaded user identity", () => {
  const commands: WorkspaceBotCommand[] = [];
  assert.equal(
    agentNameFor("user-a", commands, () => user("user-a", "Ritz")),
    "Ritz",
  );
  assert.equal(
    agentNameFor("user-b", commands, () => user("user-b", "", "someone")),
    "@someone",
  );
});

test("deduplicates active responders by user ID, not display name", () => {
  const commands = [command("bot-a", "Worker"), command("bot-b", "Worker")];
  const turns = [
    { turnId: "a", userId: "bot-a", lines: [{ finalized: false }] },
    { turnId: "b", userId: "bot-b", lines: [{ finalized: false }] },
    { turnId: "a-duplicate", userId: "bot-a", lines: [{ finalized: false }] },
  ];
  assert.deepEqual(
    respondingAgentNames(turns, commands, () => undefined),
    ["Worker", "Worker"],
  );
});

test("does not name unresolved senders", () => {
  const turns = [{ turnId: "unknown", userId: "", lines: [{ finalized: false }] }];
  assert.deepEqual(
    respondingAgentNames(turns, [], () => undefined),
    [],
  );
});
