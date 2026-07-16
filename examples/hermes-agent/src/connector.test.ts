import assert from "node:assert/strict";
import test from "node:test";

import type {
  AgentProgressPayload,
  EphemeralEventInput,
  Message,
  MessageInput,
  RealtimeEvent,
  User,
} from "../../../packages/sdk-ts/src/index.ts";
import type { HermesRunEvent } from "./hermes-client.ts";
import {
  HermesClickClackConnector,
  type ConnectorClickClackClient,
  type ConnectorHermesClient,
} from "./connector.ts";

const bot: User = {
  id: "usr_bot",
  kind: "bot",
  display_name: "Hermes",
  handle: "hermes",
  avatar_url: "",
  created_at: "2026-07-14T00:00:00Z",
};

function message(id: string, body: string, overrides: Partial<Message> = {}): Message {
  const authorId = overrides.author_id ?? "usr_human";
  return {
    id,
    workspace_id: "wsp_1",
    channel_id: "chn_1",
    author_id: authorId,
    thread_root_id: overrides.thread_root_id ?? id,
    body,
    body_format: "markdown",
    created_at: overrides.created_at ?? "2026-07-14T00:00:00Z",
    kind: "message",
    author:
      overrides.author ??
      (authorId === bot.id
        ? bot
        : {
            id: authorId,
            kind: "human",
            display_name: "Alice",
            handle: "alice",
            avatar_url: "",
            created_at: "2026-07-14T00:00:00Z",
          }),
    ...overrides,
  };
}

function eventFor(
  msg: Message,
  type = msg.parent_message_id ? "thread.reply_created" : "message.created",
): RealtimeEvent {
  return {
    id: `evt_${msg.id}`,
    cursor: `cur_${msg.id}`,
    type,
    workspace_id: msg.workspace_id,
    channel_id: msg.channel_id,
    seq: msg.channel_seq ?? 10,
    created_at: msg.created_at,
    payload: {
      message_id: msg.id,
      author_id: msg.author_id,
      ...(msg.direct_conversation_id ? { direct_conversation_id: msg.direct_conversation_id } : {}),
      ...(msg.parent_message_id ? { root_message_id: msg.thread_root_id } : {}),
    },
  };
}

type ThreadView = { root: Message; replies: Message[] };

class FakeClickClack implements ConnectorClickClackClient {
  readonly byId = new Map<string, Message>();
  readonly dmHistory = new Map<string, Message[]>();
  readonly threadHistory = new Map<string, ThreadView>();
  readonly progress: EphemeralEventInput[] = [];
  readonly dmSends: Array<{ conversationId: string; input: MessageInput }> = [];
  readonly threadSends: Array<{ rootId: string; input: { body: string; nonce?: string } }> = [];
  lastThreadOptions?: { limit?: number; latest?: boolean };
  messageGetFailuresRemaining = 0;
  dmFailuresRemaining = 0;

  messages = {
    get: async (messageId: string): Promise<Message> => {
      if (this.messageGetFailuresRemaining > 0) {
        this.messageGetFailuresRemaining -= 1;
        throw new Error("transient message fetch failure");
      }
      const value = this.byId.get(messageId);
      if (!value) throw new Error(`missing message ${messageId}`);
      return value;
    },
  };

  threads = {
    get: async (
      rootId: string,
      options: { limit?: number; latest?: boolean } = {},
    ): Promise<ThreadView> => {
      const value = this.threadHistory.get(rootId);
      if (!value) throw new Error(`missing thread ${rootId}`);
      this.lastThreadOptions = options;
      return {
        root: value.root,
        replies: options.limit === undefined ? value.replies : value.replies.slice(-options.limit),
      };
    },
    reply: async (rootId: string, input: { body: string; nonce?: string }): Promise<Message> => {
      this.threadSends.push({ rootId, input });
      const sent = message(`msg_sent_${this.threadSends.length}`, input.body, {
        author_id: bot.id,
        author: bot,
        parent_message_id: rootId,
        thread_root_id: rootId,
      });
      const thread = this.threadHistory.get(rootId);
      if (thread) thread.replies.push(sent);
      return sent;
    },
  };

  dms = {
    messages: async (
      conversationId: string,
      afterSeq?: number,
      limit?: number,
    ): Promise<Message[]> => {
      const values = this.dmHistory.get(conversationId) ?? [];
      const filtered = values.filter((entry) => (entry.channel_seq ?? 0) > (afterSeq ?? 0));
      return limit === undefined ? filtered : filtered.slice(0, limit);
    },
    sendMessage: async (conversationId: string, input: MessageInput): Promise<Message> => {
      this.dmSends.push({ conversationId, input });
      if (this.dmFailuresRemaining > 0) {
        this.dmFailuresRemaining -= 1;
        throw new Error("transient ClickClack failure");
      }
      const history = this.dmHistory.get(conversationId) ?? [];
      const sent = message(`msg_dm_sent_${this.dmSends.length}`, input.body, {
        channel_id: undefined,
        channel_seq: (history.at(-1)?.channel_seq ?? 0) + 1,
        direct_conversation_id: conversationId,
        author_id: bot.id,
        author: bot,
      });
      history.push(sent);
      this.dmHistory.set(conversationId, history);
      return sent;
    },
  };

  events = {
    publishEphemeral: async (input: EphemeralEventInput): Promise<RealtimeEvent> => {
      this.progress.push(input);
      return {
        id: `evt_progress_${this.progress.length}`,
        cursor: "",
        type: input.type,
        workspace_id: input.workspaceId,
        channel_id: input.channelId,
        created_at: "2026-07-14T00:00:00Z",
        payload: input.payload ?? {},
      };
    },
  };
}

class FakeHermes implements ConnectorHermesClient {
  readonly starts: Array<Record<string, unknown>> = [];
  readonly stopped: string[] = [];
  events: HermesRunEvent[] = [{ event: "run.completed", run_id: "run_1", output: "Hermes answer" }];
  startError?: Error;

  async startRun(input: Record<string, unknown>) {
    this.starts.push(input);
    if (this.startError) throw this.startError;
    return { runId: "run_1", status: "queued" };
  }

  async *streamRunEvents(_runId: string, _signal?: AbortSignal): AsyncGenerator<HermesRunEvent> {
    for (const event of this.events) yield event;
  }

  async stopRun(runId: string): Promise<void> {
    this.stopped.push(runId);
  }
}

class BlockingHermes extends FakeHermes {
  readonly pending = new Map<string, () => void>();
  active = 0;
  maxActive = 0;

  override async startRun(input: Record<string, unknown>) {
    this.starts.push(input);
    const runId = `run_${this.starts.length}`;
    return { runId, status: "queued" };
  }

  override async *streamRunEvents(runId: string): AsyncGenerator<HermesRunEvent> {
    this.active += 1;
    this.maxActive = Math.max(this.maxActive, this.active);
    await new Promise<void>((resolve) => this.pending.set(runId, resolve));
    this.pending.delete(runId);
    this.active -= 1;
    yield { event: "run.completed", run_id: runId, output: `answer ${runId}` };
  }

  release(runId: string): void {
    const resolve = this.pending.get(runId);
    if (!resolve) throw new Error(`run ${runId} is not pending`);
    resolve();
  }
}

class TimeoutHermes extends FakeHermes {
  override async *streamRunEvents(
    _runId: string,
    signal?: AbortSignal,
  ): AsyncGenerator<HermesRunEvent> {
    await new Promise<never>((_resolve, reject) => {
      if (signal?.aborted) {
        reject(signal.reason);
        return;
      }
      signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
    });
    throw new Error("unreachable");
  }
}

class HangingStopHermes extends TimeoutHermes {
  override async startRun(input: Record<string, unknown>) {
    this.starts.push(input);
    return { runId: `run_${this.starts.length}`, status: "queued" };
  }

  override stopRun(runId: string): Promise<void> {
    this.stopped.push(runId);
    return new Promise<void>(() => {});
  }
}

function connector(
  client: FakeClickClack,
  hermes: FakeHermes,
  logErrors: unknown[] = [],
  maxConcurrentRuns = 4,
  runTimeoutMs = 1_800_000,
  stopTimeoutMs = 5_000,
  policy: {
    allowedUserIds?: string[];
    allowedChannelIds?: string[];
    signal?: AbortSignal;
  } = {},
) {
  return new HermesClickClackConnector({
    clickclack: client,
    hermes,
    workspaceId: "wsp_1",
    bot,
    historyLimit: 20,
    maxConcurrentRuns,
    runTimeoutMs,
    stopTimeoutMs,
    allowedUserIds: new Set(policy.allowedUserIds ?? ["usr_human"]),
    allowedChannelIds: new Set(policy.allowedChannelIds ?? ["chn_1"]),
    signal: policy.signal,
    logger: {
      info: () => {},
      warn: () => {},
      error: (...values: unknown[]) => logErrors.push(values),
    },
  });
}

test("rejects a DM from a sender outside the explicit allowlist", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_denied_dm", "run a tool", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);

  await connector(client, hermes, [], 4, 1_800_000, 5_000, {
    allowedUserIds: ["usr_someone_else"],
  }).handleEvent(eventFor(current));

  assert.equal(hermes.starts.length, 0);
  assert.equal(client.dmSends.length, 0);
});

test("rejects a channel mention outside the explicit channel allowlist", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_denied_channel", "@hermes run a tool");
  client.byId.set(current.id, current);

  await connector(client, hermes, [], 4, 1_800_000, 5_000, {
    allowedChannelIds: ["chn_somewhere_else"],
  }).handleEvent(eventFor(current));

  assert.equal(hermes.starts.length, 0);
  assert.equal(client.threadSends.length, 0);
});

test("rejects malformed dual-target and cross-workspace messages", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const dualTarget = message("msg_dual_target", "hello", {
    channel_id: "chn_1",
    direct_conversation_id: "dm_1",
  });
  const wrongWorkspace = message("msg_wrong_workspace", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    workspace_id: "wsp_other",
  });
  client.byId.set(dualTarget.id, dualTarget);
  client.byId.set(wrongWorkspace.id, wrongWorkspace);

  const instance = connector(client, hermes);
  await instance.handleEvent(eventFor(dualTarget));
  await instance.handleEvent(eventFor(wrongWorkspace));

  assert.equal(hermes.starts.length, 0);
  assert.equal(client.dmSends.length, 0);
  assert.equal(client.threadSends.length, 0);
});

test("ignores self-authored, bot-authored, activity, and unmentioned channel messages", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const values = [
    message("msg_self", "hello", { author_id: bot.id, author: bot }),
    message("msg_other_bot", "hello", {
      author_id: "usr_other_bot",
      author: { ...bot, id: "usr_other_bot", handle: "other" },
    }),
    message("msg_activity", "tool", { kind: "agent_tool" }),
    message("msg_plain", "hello without mention"),
  ];
  for (const value of values) client.byId.set(value.id, value);
  const bridge = connector(client, hermes);

  for (const value of values) await bridge.handleEvent(eventFor(value));

  assert.equal(hermes.starts.length, 0);
  assert.equal(client.threadSends.length, 0);
});

test("answers DMs with bounded history, safe progress, and a deterministic nonce", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const oldUser = message("msg_old_user", "Earlier question", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 8,
  });
  const oldBot = message("msg_old_bot", "Earlier answer", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    author_id: bot.id,
    author: bot,
    channel_seq: 9,
  });
  const current = message("msg_current", "Latest question", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 10,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [oldUser, oldBot, current]);
  hermes.events = [
    { event: "reasoning.available", text: "private chain of thought" },
    { event: "tool.started", tool: "web_search", preview: "secret query" },
    { event: "tool.completed", tool: "web_search", error: false },
    { event: "message.delta", delta: "Herm" },
    { event: "run.completed", output: "Hermes answer" },
  ];

  await connector(client, hermes).handleEvent(eventFor(current));

  assert.equal(hermes.starts.length, 1);
  assert.deepEqual(hermes.starts[0]?.conversationHistory, [
    { role: "user", content: "Earlier question" },
    { role: "assistant", content: "Earlier answer" },
  ]);
  assert.equal(hermes.starts[0]?.input, "Latest question");
  assert.equal(hermes.starts[0]?.sessionId, "cc:wsp_1:dm:dm_1");
  assert.equal(hermes.starts[0]?.sessionKey, "cc:wsp_1:dm:dm_1");
  assert.deepEqual(client.dmSends, [
    { conversationId: "dm_1", input: { body: "Hermes answer", nonce: "hermes:msg_current" } },
  ]);
  const serializedProgress = JSON.stringify(client.progress);
  assert.doesNotMatch(serializedProgress, /private chain of thought|secret query/);
  assert.match(serializedProgress, /web_search/);
  assert.ok(
    client.progress.every(
      (entry) =>
        entry.type !== "agent.progress" ||
        (entry.payload as AgentProgressPayload).turn_id === current.id,
    ),
  );
});

test("a channel mention starts a thread and strips only the bot mention", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_root", "Hey @hermes, inspect `@hermes-file` please");
  client.byId.set(current.id, current);

  await connector(client, hermes).handleEvent(eventFor(current));

  assert.equal(hermes.starts[0]?.input, "Alice: Hey , inspect `@hermes-file` please");
  assert.equal(hermes.starts[0]?.sessionId, "cc:wsp_1:thread:msg_root");
  assert.deepEqual(client.threadSends, [
    { rootId: "msg_root", input: { body: "Hermes answer", nonce: "hermes:msg_root" } },
  ]);
});

test("continues an active bot thread without another mention and rebuilds group history", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const root = message("msg_root", "@hermes first question");
  const priorBot = message("msg_bot_reply", "First answer", {
    author_id: bot.id,
    author: bot,
    parent_message_id: root.id,
    thread_root_id: root.id,
  });
  const current = message("msg_followup", "follow up", {
    parent_message_id: priorBot.id,
    thread_root_id: root.id,
  });
  client.byId.set(current.id, current);
  client.threadHistory.set(root.id, { root, replies: [priorBot, current] });

  await connector(client, hermes).handleEvent(eventFor(current));

  assert.equal(hermes.starts.length, 1);
  assert.deepEqual(hermes.starts[0]?.conversationHistory, [
    { role: "user", content: "Alice: first question" },
    { role: "assistant", content: "First answer" },
  ]);
  assert.equal(hermes.starts[0]?.input, "Alice: follow up");
  assert.equal(client.threadSends[0]?.rootId, root.id);
});

test("recognizes bot participation beyond the old 100-reply thread window", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const root = message("msg_long_root", "long thread");
  const replies = Array.from({ length: 149 }, (_, index) =>
    message(`msg_long_${index + 1}`, `reply ${index + 1}`, {
      author_id: index === 119 ? bot.id : "usr_human",
      author: index === 119 ? bot : undefined,
      parent_message_id: root.id,
      thread_root_id: root.id,
    }),
  );
  const current = message("msg_long_current", "continue", {
    parent_message_id: root.id,
    thread_root_id: root.id,
  });
  replies.push(current);
  client.byId.set(current.id, current);
  client.threadHistory.set(root.id, { root, replies });

  await connector(client, hermes).handleEvent(eventFor(current));

  assert.deepEqual(client.lastThreadOptions, { limit: 200, latest: true });
  assert.equal(hermes.starts.length, 1);
  assert.equal(client.threadSends[0]?.rootId, root.id);
});

test("ignores a thread where the bot has not participated and is not mentioned", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const root = message("msg_root", "human thread");
  const current = message("msg_followup", "another human reply", {
    parent_message_id: root.id,
    thread_root_id: root.id,
  });
  client.byId.set(current.id, current);
  client.threadHistory.set(root.id, { root, replies: [current] });

  await connector(client, hermes).handleEvent(eventFor(current));

  assert.equal(hermes.starts.length, 0);
});

test("releases a failed event preparation so a reconnect can retry it", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_prepare_retry", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);
  client.messageGetFailuresRemaining = 1;
  const instance = connector(client, hermes);

  await assert.rejects(() => instance.scheduleEvent(eventFor(current)), /transient message fetch/);
  const retried = await instance.scheduleEvent(eventFor(current));
  await retried.completion;

  assert.equal(hermes.starts.length, 1);
  assert.equal(client.dmSends.length, 1);
});

test("scheduled events run different conversations concurrently", async () => {
  const client = new FakeClickClack();
  const hermes = new BlockingHermes();
  const first = message("msg_dm_1", "first", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  const second = message("msg_dm_2", "second", {
    channel_id: undefined,
    direct_conversation_id: "dm_2",
    channel_seq: 1,
  });
  client.byId.set(first.id, first);
  client.byId.set(second.id, second);
  client.dmHistory.set("dm_1", [first]);
  client.dmHistory.set("dm_2", [second]);
  const instance = connector(client, hermes);

  await Promise.all([
    instance.scheduleEvent(eventFor(first)),
    instance.scheduleEvent(eventFor(second)),
  ]);
  await waitForCondition(() => hermes.pending.size === 2);

  assert.equal(hermes.maxActive, 2);
  hermes.release("run_1");
  hermes.release("run_2");
  await instance.waitForIdle();
  assert.equal(client.dmSends.length, 2);
});

test("scheduled events respect the global run limit", async () => {
  const client = new FakeClickClack();
  const hermes = new BlockingHermes();
  const first = message("msg_limited_1", "first", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  const second = message("msg_limited_2", "second", {
    channel_id: undefined,
    direct_conversation_id: "dm_2",
    channel_seq: 1,
  });
  client.byId.set(first.id, first);
  client.byId.set(second.id, second);
  client.dmHistory.set("dm_1", [first]);
  client.dmHistory.set("dm_2", [second]);
  const instance = connector(client, hermes, [], 1);

  await Promise.all([
    instance.scheduleEvent(eventFor(first)),
    instance.scheduleEvent(eventFor(second)),
  ]);
  await waitForCondition(() => hermes.pending.has("run_1"));
  assert.equal(hermes.starts.length, 1);
  assert.equal(hermes.maxActive, 1);

  hermes.release("run_1");
  await waitForCondition(() => hermes.pending.has("run_2"));
  assert.equal(hermes.maxActive, 1);

  hermes.release("run_2");
  await instance.waitForIdle();
});

test("scheduled events serialize one conversation and include the completed queued reply", async () => {
  const client = new FakeClickClack();
  const hermes = new BlockingHermes();
  const first = message("msg_dm_1", "first", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  const second = message("msg_dm_2", "second", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 2,
  });
  client.byId.set(first.id, first);
  client.byId.set(second.id, second);
  client.dmHistory.set("dm_1", [first, second]);
  const instance = connector(client, hermes);

  await Promise.all([
    instance.scheduleEvent(eventFor(first)),
    instance.scheduleEvent(eventFor(second)),
  ]);
  await waitForCondition(() => hermes.pending.has("run_1"));
  assert.equal(hermes.starts.length, 1);

  hermes.release("run_1");
  await waitForCondition(() => hermes.pending.has("run_2"));
  assert.equal(hermes.maxActive, 1);
  assert.deepEqual(hermes.starts[1]?.conversationHistory, [
    { role: "user", content: "first" },
    { role: "assistant", content: "answer run_1" },
  ]);

  hermes.release("run_2");
  await instance.waitForIdle();
});

test("retries transient final reply delivery with one deterministic nonce", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_retry", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);
  client.dmFailuresRemaining = 2;

  await connector(client, hermes).handleEvent(eventFor(current));

  assert.equal(client.dmSends.length, 3);
  assert.deepEqual(
    client.dmSends.map((send) => send.input.nonce),
    ["hermes:msg_retry", "hermes:msg_retry", "hermes:msg_retry"],
  );
  assert.equal(client.dmSends[2]?.input.body, "Hermes answer");
  const lifecycle = client.progress
    .map((entry) => entry.payload as AgentProgressPayload)
    .findLast((payload) => payload.op === "finalize" && payload.line.id === "lifecycle");
  if (!lifecycle || lifecycle.op !== "finalize") throw new Error("missing lifecycle finalization");
  assert.equal(lifecycle.line.status, "completed");
});

test("rejects completion when final reply delivery exhausts retries", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_reply_exhausted", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);
  client.dmFailuresRemaining = 3;

  const scheduled = await connector(client, hermes).scheduleEvent(eventFor(current));

  await assert.rejects(scheduled.completion, /reply delivery exhausted retries/i);
  assert.equal(client.dmSends.length, 3);
});

test("rejects completion when generic failure delivery exhausts retries", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_failure_exhausted", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);
  client.dmFailuresRemaining = 3;
  hermes.startError = new Error("provider failed");

  const scheduled = await connector(client, hermes).scheduleEvent(eventFor(current));

  await assert.rejects(scheduled.completion, /reply delivery exhausted retries/i);
  assert.equal(client.dmSends.length, 3);
});

test("rejects active and queued completions interrupted by shutdown", async () => {
  const client = new FakeClickClack();
  const hermes = new TimeoutHermes();
  const abort = new AbortController();
  const first = message("msg_abort_1", "first", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  const second = message("msg_abort_2", "second", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 2,
  });
  client.byId.set(first.id, first);
  client.byId.set(second.id, second);
  client.dmHistory.set("dm_1", [first, second]);
  const instance = connector(client, hermes, [], 4, 1_800_000, 5_000, {
    signal: abort.signal,
  });

  const firstTask = await instance.scheduleEvent(eventFor(first));
  const secondTask = await instance.scheduleEvent(eventFor(second));
  await waitForCondition(() => hermes.starts.length === 1);
  abort.abort(new Error("connector shutdown"));

  await assert.rejects(firstTask.completion, /connector shutdown/i);
  await assert.rejects(secondTask.completion, /connector shutdown/i);
  assert.equal(hermes.starts.length, 1);
  assert.equal(client.dmSends.length, 0);
});

test("times out a stalled run, stops it, and reports a generic failure", async () => {
  const client = new FakeClickClack();
  const hermes = new TimeoutHermes();
  const current = message("msg_timeout", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);

  await connector(client, hermes, [], 4, 20).handleEvent(eventFor(current));

  assert.deepEqual(hermes.stopped, ["run_1"]);
  assert.equal(client.dmSends.length, 1);
  assert.match(client.dmSends[0]?.input.body ?? "", /couldn't complete/i);
});

test("bounds a hanging stop request and releases the global run slot", async () => {
  const client = new FakeClickClack();
  const hermes = new HangingStopHermes();
  const first = message("msg_stop_1", "first", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  const second = message("msg_stop_2", "second", {
    channel_id: undefined,
    direct_conversation_id: "dm_2",
    channel_seq: 1,
  });
  client.byId.set(first.id, first);
  client.byId.set(second.id, second);
  client.dmHistory.set("dm_1", [first]);
  client.dmHistory.set("dm_2", [second]);
  const instance = connector(client, hermes, [], 1, 20, 20);

  const firstTask = await instance.scheduleEvent(eventFor(first));
  const secondTask = await instance.scheduleEvent(eventFor(second));
  await Promise.all([firstTask.completion, secondTask.completion]);
  await instance.waitForIdle();

  assert.equal(hermes.starts.length, 2);
  assert.deepEqual(hermes.stopped, ["run_1", "run_2"]);
  assert.equal(client.dmSends.length, 2);
});

test("marks string-valued Hermes tool errors as failed progress", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const current = message("msg_tool_error", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);
  hermes.events = [
    { event: "tool.started", run_id: "run_1", tool: "shell" },
    { event: "tool.completed", run_id: "run_1", tool: "shell", error: "command failed" },
    { event: "run.completed", run_id: "run_1", output: "done" },
  ];

  await connector(client, hermes).handleEvent(eventFor(current));

  const toolFinal = client.progress
    .map((entry) => entry.payload as AgentProgressPayload)
    .find(
      (payload) =>
        payload.op === "finalize" &&
        payload.line.kind === "tool" &&
        payload.line.status === "failed",
    );
  assert.ok(toolFinal);
});

test("reports a generic chat failure without exposing connector errors", async () => {
  const client = new FakeClickClack();
  const hermes = new FakeHermes();
  const errors: unknown[] = [];
  const current = message("msg_current", "hello", {
    channel_id: undefined,
    direct_conversation_id: "dm_1",
    channel_seq: 1,
  });
  client.byId.set(current.id, current);
  client.dmHistory.set("dm_1", [current]);
  hermes.startError = new Error("provider secret diagnostic");

  await connector(client, hermes, errors).handleEvent(eventFor(current));

  assert.equal(errors.length, 1);
  assert.equal(client.dmSends.length, 1);
  assert.match(client.dmSends[0]?.input.body ?? "", /couldn't complete/i);
  assert.doesNotMatch(client.dmSends[0]?.input.body ?? "", /provider secret diagnostic/);
});

async function waitForCondition(predicate: () => boolean): Promise<void> {
  const deadline = Date.now() + 2_000;
  while (!predicate()) {
    if (Date.now() >= deadline) throw new Error("condition timed out");
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
}
