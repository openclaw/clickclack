import type {
  AgentProgressLine,
  AgentProgressPayload,
  EphemeralEventInput,
  Message,
  MessageInput,
  RealtimeEvent,
  User,
} from "@clickclack/sdk-ts";

import type { HermesConversationMessage, HermesRun, HermesRunEvent } from "./hermes-client.ts";

export type ConnectorClickClackClient = {
  messages: {
    get(messageId: string): Promise<Message>;
  };
  threads: {
    get(
      rootId: string,
      options?: { limit?: number; latest?: boolean },
    ): Promise<{ root: Message; replies: Message[] }>;
    reply(rootId: string, input: { body: string; nonce?: string }): Promise<Message>;
  };
  dms: {
    messages(conversationId: string, afterSeq?: number, limit?: number): Promise<Message[]>;
    sendMessage(conversationId: string, input: MessageInput): Promise<Message>;
  };
  events: {
    publishEphemeral(input: EphemeralEventInput): Promise<RealtimeEvent>;
  };
};

export type ConnectorHermesClient = {
  startRun(input: {
    input: string;
    conversationHistory?: HermesConversationMessage[];
    instructions?: string;
    sessionId: string;
    sessionKey: string;
    signal?: AbortSignal;
  }): Promise<HermesRun>;
  streamRunEvents(runId: string, signal?: AbortSignal): AsyncGenerator<HermesRunEvent>;
  stopRun(runId: string, signal?: AbortSignal): Promise<void>;
};

export type ConnectorLogger = {
  info(...values: unknown[]): void;
  warn(...values: unknown[]): void;
  error(...values: unknown[]): void;
};

export type HermesClickClackConnectorOptions = {
  clickclack: ConnectorClickClackClient;
  hermes: ConnectorHermesClient;
  workspaceId: string;
  bot: User;
  historyLimit?: number;
  maxReplyChars?: number;
  maxConcurrentRuns?: number;
  runTimeoutMs?: number;
  stopTimeoutMs?: number;
  instructions?: string;
  signal?: AbortSignal;
  logger?: ConnectorLogger;
};

type ReplyTarget =
  | { kind: "dm"; conversationId: string }
  | { kind: "thread"; channelId: string; rootId: string };

type ConversationContext = {
  input: string;
  history: HermesConversationMessage[];
  sessionKey: string;
  target: ReplyTarget;
};

type PreparedEvent = {
  event: RealtimeEvent;
  message: Message;
  conversationKey: string;
};

const DEFAULT_HISTORY_LIMIT = 20;
const DEFAULT_MAX_REPLY_CHARS = 100_000;
const DEFAULT_MAX_CONCURRENT_RUNS = 4;
const DEFAULT_RUN_TIMEOUT_MS = 1_800_000;
const DEFAULT_STOP_TIMEOUT_MS = 5_000;
const DEFAULT_INSTRUCTIONS =
  "You are responding inside ClickClack. Reply in clear Markdown. Never expose hidden chain-of-thought or private tool arguments.";
const GENERIC_FAILURE =
  "Hermes couldn't complete this run. Check the connector logs and try again.";

class ReplyDeliveryError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "ReplyDeliveryError";
  }
}

const defaultLogger: ConnectorLogger = {
  info: (...values) => console.log(...values),
  warn: (...values) => console.warn(...values),
  error: (...values) => console.error(...values),
};

export class HermesClickClackConnector {
  private readonly clickclack: ConnectorClickClackClient;
  private readonly hermes: ConnectorHermesClient;
  private readonly workspaceId: string;
  private readonly bot: User;
  private readonly historyLimit: number;
  private readonly maxReplyChars: number;
  private readonly maxConcurrentRuns: number;
  private readonly runTimeoutMs: number;
  private readonly stopTimeoutMs: number;
  private readonly instructions: string;
  private readonly signal?: AbortSignal;
  private readonly logger: ConnectorLogger;
  private readonly inFlight = new Set<string>();
  private readonly conversationQueues = new Map<string, Promise<void>>();
  private readonly runWaiters: Array<() => void> = [];
  private activeRuns = 0;

  constructor(options: HermesClickClackConnectorOptions) {
    this.clickclack = options.clickclack;
    this.hermes = options.hermes;
    this.workspaceId = options.workspaceId;
    this.bot = options.bot;
    this.historyLimit = options.historyLimit ?? DEFAULT_HISTORY_LIMIT;
    this.maxReplyChars = options.maxReplyChars ?? DEFAULT_MAX_REPLY_CHARS;
    this.maxConcurrentRuns = options.maxConcurrentRuns ?? DEFAULT_MAX_CONCURRENT_RUNS;
    this.runTimeoutMs = options.runTimeoutMs ?? DEFAULT_RUN_TIMEOUT_MS;
    this.stopTimeoutMs = options.stopTimeoutMs ?? DEFAULT_STOP_TIMEOUT_MS;
    this.instructions = options.instructions?.trim() || DEFAULT_INSTRUCTIONS;
    this.signal = options.signal;
    this.logger = options.logger ?? defaultLogger;
    if (this.historyLimit < 0 || !Number.isSafeInteger(this.historyLimit)) {
      throw new Error("historyLimit must be a non-negative integer");
    }
    if (this.maxReplyChars < 1 || !Number.isSafeInteger(this.maxReplyChars)) {
      throw new Error("maxReplyChars must be a positive integer");
    }
    if (this.maxConcurrentRuns < 1 || !Number.isSafeInteger(this.maxConcurrentRuns)) {
      throw new Error("maxConcurrentRuns must be a positive integer");
    }
    if (this.runTimeoutMs < 1 || !Number.isSafeInteger(this.runTimeoutMs)) {
      throw new Error("runTimeoutMs must be a positive integer");
    }
    if (this.stopTimeoutMs < 1 || !Number.isSafeInteger(this.stopTimeoutMs)) {
      throw new Error("stopTimeoutMs must be a positive integer");
    }
  }

  async handleEvent(event: RealtimeEvent): Promise<void> {
    const prepared = await this.prepareEvent(event);
    if (prepared) await this.executePrepared(prepared);
  }

  async scheduleEvent(event: RealtimeEvent): Promise<{ completion: Promise<void> }> {
    const prepared = await this.prepareEvent(event);
    if (!prepared) return { completion: Promise.resolve() };

    const previous = this.conversationQueues.get(prepared.conversationKey) ?? Promise.resolve();
    const task = previous.catch(() => undefined).then(() => this.executePrepared(prepared));
    this.conversationQueues.set(prepared.conversationKey, task);
    const cleanup = () => {
      if (this.conversationQueues.get(prepared.conversationKey) === task) {
        this.conversationQueues.delete(prepared.conversationKey);
      }
    };
    void task.then(cleanup, cleanup);
    return { completion: task };
  }

  async waitForIdle(): Promise<void> {
    while (this.conversationQueues.size > 0) {
      await Promise.all(this.conversationQueues.values());
    }
  }

  private async prepareEvent(event: RealtimeEvent): Promise<PreparedEvent | undefined> {
    if (event.type !== "message.created" && event.type !== "thread.reply_created") return undefined;
    const messageId = payloadString(event, "message_id");
    if (!messageId || this.inFlight.has(messageId)) return undefined;
    if (payloadString(event, "author_id") === this.bot.id) return undefined;

    this.inFlight.add(messageId);
    try {
      const message = await this.clickclack.messages.get(messageId);
      if (!isHumanMessage(message, this.bot.id)) {
        this.inFlight.delete(messageId);
        return undefined;
      }
      const conversationKey = this.conversationKeyFor(message);
      if (!conversationKey) {
        this.inFlight.delete(messageId);
        return undefined;
      }
      return { event, message, conversationKey };
    } catch (error) {
      this.inFlight.delete(messageId);
      this.logger.error("ClickClack event preparation failed", error);
      throw error;
    }
  }

  private conversationKeyFor(message: Message): string | undefined {
    if (message.direct_conversation_id) {
      return `cc:${this.workspaceId}:dm:${message.direct_conversation_id}`;
    }
    if (!message.channel_id) return undefined;
    if (!message.parent_message_id && !containsMention(message.body, this.bot.handle))
      return undefined;
    return `cc:${this.workspaceId}:thread:${message.thread_root_id || message.id}`;
  }

  private async executePrepared(prepared: PreparedEvent): Promise<void> {
    try {
      if (this.signal?.aborted) return;
      const context = await this.resolveConversation(prepared.event, prepared.message);
      if (!context) return;
      await this.withRunSlot(() => this.runAndReply(prepared.message, context));
    } catch (error) {
      this.logger.error("ClickClack event handling failed", error);
      throw error;
    } finally {
      this.inFlight.delete(prepared.message.id);
    }
  }

  private async withRunSlot(task: () => Promise<void>): Promise<void> {
    await this.acquireRunSlot();
    try {
      if (!this.signal?.aborted) await task();
    } finally {
      this.releaseRunSlot();
    }
  }

  private async acquireRunSlot(): Promise<void> {
    if (this.activeRuns < this.maxConcurrentRuns) {
      this.activeRuns += 1;
      return;
    }
    await new Promise<void>((resolve) => this.runWaiters.push(resolve));
  }

  private releaseRunSlot(): void {
    const next = this.runWaiters.shift();
    if (next) {
      next();
      return;
    }
    this.activeRuns -= 1;
  }

  private async resolveConversation(
    event: RealtimeEvent,
    message: Message,
  ): Promise<ConversationContext | undefined> {
    const directConversationId = message.direct_conversation_id;
    if (directConversationId) {
      const sequence = message.channel_seq ?? event.seq ?? 0;
      const afterSeq = Math.max(0, sequence - this.historyLimit - 1);
      const messages = await this.clickclack.dms.messages(
        directConversationId,
        afterSeq,
        this.historyLimit + 1,
      );
      return {
        input: message.body.trim(),
        history: mapHistory(messages, message.id, this.bot, this.historyLimit, false),
        sessionKey: `cc:${this.workspaceId}:dm:${directConversationId}`,
        target: { kind: "dm", conversationId: directConversationId },
      };
    }

    if (!message.channel_id) return undefined;
    const mentioned = containsMention(message.body, this.bot.handle);
    const rootId = message.thread_root_id || message.id;
    if (!message.parent_message_id) {
      if (!mentioned) return undefined;
      return {
        input: formatHumanInput(message, this.bot.handle, true),
        history: [],
        sessionKey: `cc:${this.workspaceId}:thread:${rootId}`,
        target: { kind: "thread", channelId: message.channel_id, rootId },
      };
    }

    const thread = await this.clickclack.threads.get(rootId, { limit: 200, latest: true });
    const messages = [thread.root, ...thread.replies];
    if (!mentioned && !messages.some((entry) => entry.author_id === this.bot.id)) return undefined;
    return {
      input: formatHumanInput(message, this.bot.handle, true),
      history: mapHistory(messages, message.id, this.bot, this.historyLimit, true),
      sessionKey: `cc:${this.workspaceId}:thread:${rootId}`,
      target: { kind: "thread", channelId: message.channel_id, rootId },
    };
  }

  private async runAndReply(message: Message, context: ConversationContext): Promise<void> {
    let runId: string | undefined;
    const progress = new ProgressPublisher({
      clickclack: this.clickclack,
      workspaceId: this.workspaceId,
      target: context.target,
      turnId: message.id,
      logger: this.logger,
    });
    await progress.lifecycle("Hermes is working", "running");

    const runAbort = new AbortController();
    const abortFromParent = () => runAbort.abort(this.signal?.reason);
    this.signal?.addEventListener("abort", abortFromParent, { once: true });
    const timeout = setTimeout(
      () => runAbort.abort(new Error(`Hermes run exceeded ${this.runTimeoutMs}ms`)),
      this.runTimeoutMs,
    );

    try {
      const run = await this.hermes.startRun({
        input: context.input,
        conversationHistory: context.history,
        instructions: this.instructions,
        sessionId: context.sessionKey,
        sessionKey: context.sessionKey,
        signal: runAbort.signal,
      });
      runId = run.runId;
      const output = await this.consumeRun(run.runId, progress, runAbort.signal);
      const reply = normalizeReply(output, this.maxReplyChars);
      await this.sendWithRetry(context.target, message.id, reply);
      await progress.finish("completed");
    } catch (error) {
      if (runId && runAbort.signal.aborted) await this.stopQuietly(runId);
      if (this.signal?.aborted) return;

      this.logger.error("Hermes run or reply delivery failed", error);
      await progress.finish("failed");
      if (error instanceof ReplyDeliveryError) return;
      try {
        await this.sendWithRetry(context.target, message.id, GENERIC_FAILURE);
      } catch (deliveryError) {
        this.logger.error("ClickClack failure reply delivery exhausted retries", deliveryError);
      }
    } finally {
      clearTimeout(timeout);
      this.signal?.removeEventListener("abort", abortFromParent);
    }
  }

  private async consumeRun(
    runId: string,
    progress: ProgressPublisher,
    signal: AbortSignal,
  ): Promise<string> {
    let completedOutput: string | undefined;
    for await (const event of this.hermes.streamRunEvents(runId, signal)) {
      switch (event.event) {
        case "reasoning.available":
          break;
        case "tool.started":
          await progress.toolStarted(safeToolName(event.tool));
          break;
        case "tool.completed":
          await progress.toolCompleted(safeToolName(event.tool), hasToolError(event.error));
          break;
        case "message.delta":
          await progress.writing();
          break;
        case "approval.request":
          await progress.approvalRequired();
          break;
        case "run.completed":
          completedOutput = typeof event.output === "string" ? event.output : "";
          break;
        case "run.failed":
          throw new Error("Hermes run reported failure");
        case "run.cancelled":
          throw new Error("Hermes run was cancelled");
      }
    }
    if (completedOutput === undefined) {
      throw new Error("Hermes run event stream ended before a terminal event");
    }
    return completedOutput;
  }

  private async sendWithRetry(
    target: ReplyTarget,
    sourceMessageId: string,
    body: string,
  ): Promise<void> {
    const nonce = `hermes:${sourceMessageId}`;
    let lastError: unknown;
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      try {
        if (target.kind === "dm") {
          await this.clickclack.dms.sendMessage(target.conversationId, { body, nonce });
        } else {
          await this.clickclack.threads.reply(target.rootId, { body, nonce });
        }
        return;
      } catch (error) {
        lastError = error;
        if (attempt < 3) await sleepWithSignal(100 * 2 ** (attempt - 1), this.signal);
      }
    }
    throw new ReplyDeliveryError("ClickClack reply delivery exhausted retries", {
      cause: lastError,
    });
  }

  private async stopQuietly(runId: string): Promise<void> {
    const stopAbort = new AbortController();
    let timeout: ReturnType<typeof setTimeout> | undefined;
    try {
      const outcome = await Promise.race([
        this.hermes.stopRun(runId, stopAbort.signal).then(() => "stopped" as const),
        new Promise<"timeout">((resolve) => {
          timeout = setTimeout(() => {
            stopAbort.abort(new Error(`Hermes run stop timed out after ${this.stopTimeoutMs}ms`));
            resolve("timeout");
          }, this.stopTimeoutMs);
        }),
      ]);
      if (outcome === "timeout") {
        this.logger.warn(`Timed out stopping Hermes run ${runId}`);
      }
    } catch (error) {
      this.logger.warn("Failed to stop Hermes run during shutdown", error);
    } finally {
      if (timeout) clearTimeout(timeout);
    }
  }
}

class ProgressPublisher {
  private readonly clickclack: ConnectorClickClackClient;
  private readonly workspaceId: string;
  private readonly target: ReplyTarget;
  private readonly turnId: string;
  private readonly logger: ConnectorLogger;
  private sequence = 0;
  private toolCounter = 0;
  private writingPublished = false;
  private approvalPublished = false;
  private readonly activeTools = new Map<string, string[]>();

  constructor(options: {
    clickclack: ConnectorClickClackClient;
    workspaceId: string;
    target: ReplyTarget;
    turnId: string;
    logger: ConnectorLogger;
  }) {
    this.clickclack = options.clickclack;
    this.workspaceId = options.workspaceId;
    this.target = options.target;
    this.turnId = options.turnId;
    this.logger = options.logger;
  }

  lifecycle(text: string, status: string): Promise<void> {
    return this.publish("append", { id: "lifecycle", kind: "thinking", text, status });
  }

  async writing(): Promise<void> {
    if (this.writingPublished) return;
    this.writingPublished = true;
    await this.publish("update", {
      id: "lifecycle",
      kind: "thinking",
      text: "Hermes is writing a reply",
      status: "running",
    });
  }

  async approvalRequired(): Promise<void> {
    if (this.approvalPublished) return;
    this.approvalPublished = true;
    await this.publish("append", {
      id: "approval",
      kind: "lifecycle",
      text: "Approval required in Hermes",
      status: "waiting",
    });
  }

  async toolStarted(toolName: string): Promise<void> {
    const id = `tool-${++this.toolCounter}`;
    const ids = this.activeTools.get(toolName) ?? [];
    ids.push(id);
    this.activeTools.set(toolName, ids);
    await this.publish("append", {
      id,
      kind: "tool",
      tool_name: toolName,
      status: "running",
    });
  }

  async toolCompleted(toolName: string, failed: boolean): Promise<void> {
    const ids = this.activeTools.get(toolName);
    const id = ids?.pop();
    if (!id) return;
    if (ids?.length === 0) this.activeTools.delete(toolName);
    await this.publish("finalize", {
      id,
      kind: "tool",
      tool_name: toolName,
      status: failed ? "failed" : "completed",
    });
  }

  finish(status: "completed" | "failed"): Promise<void> {
    return this.publish("finalize", { id: "lifecycle", kind: "thinking", status });
  }

  private async publish(
    op: "append" | "finalize" | "update",
    line: AgentProgressLine,
  ): Promise<void> {
    const payload: AgentProgressPayload = {
      turn_id: this.turnId,
      seq: ++this.sequence,
      op,
      line,
    };
    const input: EphemeralEventInput = {
      workspaceId: this.workspaceId,
      ...(this.target.kind === "dm"
        ? { directConversationId: this.target.conversationId }
        : { channelId: this.target.channelId }),
      type: "agent.progress",
      payload,
    };
    try {
      await this.clickclack.events.publishEphemeral(input);
    } catch (error) {
      this.logger.warn("ClickClack progress publish failed", error);
    }
  }
}

function payloadString(event: RealtimeEvent, key: string): string {
  const value = event.payload[key];
  return typeof value === "string" ? value : "";
}

function isHumanMessage(message: Message, botId: string): boolean {
  return (
    message.author_id !== botId &&
    message.author?.kind !== "bot" &&
    (message.kind === undefined || message.kind === "message") &&
    !message.deleted_at &&
    Boolean(message.body.trim())
  );
}

function containsMention(body: string, handle: string): boolean {
  return mentionPattern(handle).test(body);
}

function stripMention(body: string, handle: string): string {
  return body.replace(mentionPattern(handle), "$1").trim();
}

function mentionPattern(handle: string): RegExp {
  const escaped = handle.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
  return new RegExp(`(^|\\s)@${escaped}(?=$|[\\s,.:;!?])`, "giu");
}

function formatHumanInput(message: Message, botHandle: string, includeAuthor: boolean): string {
  const body = stripMention(message.body, botHandle);
  if (!includeAuthor) return body;
  const author =
    message.author?.display_name?.trim() || message.author?.handle?.trim() || message.author_id;
  return `${author}: ${body}`;
}

function mapHistory(
  messages: Message[],
  currentMessageId: string,
  bot: User,
  limit: number,
  includeHumanAuthors: boolean,
): HermesConversationMessage[] {
  const currentIndex = messages.findIndex((message) => message.id === currentMessageId);
  const beforeCurrent = currentIndex === -1 ? messages : messages.slice(0, currentIndex);
  const completedQueuedReplies =
    currentIndex === -1
      ? []
      : messages.slice(currentIndex + 1).filter((message) => message.author_id === bot.id);
  const prior = [...beforeCurrent, ...completedQueuedReplies]
    .filter(
      (message) =>
        !message.deleted_at &&
        (message.kind === undefined || message.kind === "message") &&
        Boolean(message.body.trim()) &&
        (message.author_id === bot.id || message.author?.kind !== "bot"),
    )
    .slice(-limit);
  return prior.map((message) =>
    message.author_id === bot.id
      ? { role: "assistant", content: message.body.trim() }
      : {
          role: "user",
          content: formatHumanInput(message, bot.handle, includeHumanAuthors),
        },
  );
}

function hasToolError(value: string | boolean | undefined): boolean {
  return value === true || (typeof value === "string" && value.trim().length > 0);
}

async function sleepWithSignal(milliseconds: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) return;
  await new Promise<void>((resolve) => {
    const timer = setTimeout(done, milliseconds);
    function done() {
      clearTimeout(timer);
      signal?.removeEventListener("abort", done);
      resolve();
    }
    signal?.addEventListener("abort", done, { once: true });
  });
}

function safeToolName(value: unknown): string {
  if (typeof value !== "string") return "tool";
  const normalized = [...value]
    .filter((character) => {
      const codePoint = character.codePointAt(0) ?? 0;
      return codePoint > 31 && codePoint !== 127;
    })
    .join("")
    .trim();
  return normalized.slice(0, 80) || "tool";
}

function normalizeReply(output: string, maxChars: number): string {
  const trimmed = output.trim();
  if (!trimmed) return "Hermes completed without a text response.";
  if (trimmed.length <= maxChars) return trimmed;
  return `${trimmed.slice(0, maxChars)}\n\n_[Response truncated by the ClickClack connector]_`;
}
