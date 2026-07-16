import assert from "node:assert/strict";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { copyFile, cp, mkdir, mkdtemp, rm } from "node:fs/promises";
import {
  createServer as createHttpServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import { createServer as createNetServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { ClickClackClient, type RealtimeEvent } from "@clickclack/sdk-ts";

type CapturedRun = {
  runId: string;
  authorization?: string;
  sessionKey?: string;
  body: Record<string, unknown>;
};

type ManagedProcess = {
  child: ChildProcessWithoutNullStreams;
  stdout: string;
  stderr: string;
  spawnError?: Error;
};

const repositoryRoot = fileURLToPath(new URL("../../..", import.meta.url));
const connectorEntry = fileURLToPath(new URL("./index.ts", import.meta.url));
const hermesSecret = "integration-hermes-key";

async function main(): Promise<void> {
  if (typeof WebSocket === "undefined") {
    throw new Error("The Hermes connector integration test requires Node.js 24 WebSocket support");
  }

  const temp = await mkdtemp(join(tmpdir(), "clickclack-hermes-e2e-"));
  const clickclackPort = await reservePort();
  const hermesPort = await reservePort();
  const clickclackBaseUrl = `http://127.0.0.1:${clickclackPort}`;
  let clickclack: ManagedProcess | undefined;
  let connector: ManagedProcess | undefined;
  let observer: WebSocket | undefined;
  let hermes: Awaited<ReturnType<typeof startMockHermes>> | undefined;

  try {
    clickclack = await startClickClack(clickclackPort, temp);
    await waitFor(
      async () => {
        throwIfSpawnFailed(clickclack!, "ClickClack");
        return (await fetch(`${clickclackBaseUrl}/readyz`)).ok;
      },
      30_000,
      "ClickClack readiness",
    );

    const human = new ClickClackClient({ baseUrl: clickclackBaseUrl });
    const [owner, workspaces] = await Promise.all([human.me(), human.workspaces.list()]);
    const workspace = workspaces[0];
    assert.ok(workspace, "dev bootstrap did not create a workspace");
    const channels = await human.channels.list(workspace.id);
    const channel = channels[0];
    assert.ok(channel, "dev bootstrap did not create a channel");

    const created = await human.bots.create(workspace.id, {
      display_name: "Hermes Integration Bot",
      handle: "hermes",
      token_name: "integration",
      scopes: ["bot:write"],
    });
    assert.ok(created.bot_token.token, "bot creation did not reveal the initial token");

    hermes = await startMockHermes(hermesPort);

    const observedEvents: RealtimeEvent[] = [];
    const initial = await human.events.list({ workspaceId: workspace.id, includeTail: true });
    observer = human.events.subscribe({
      workspaceId: workspace.id,
      afterCursor: initial.tailCursor,
      onEvent: (event) => observedEvents.push(event),
    });
    await waitForWebSocketOpen(observer);

    connector = startManagedProcess(
      process.execPath,
      ["--experimental-strip-types", connectorEntry],
      repositoryRoot,
      {
        ...process.env,
        CLICKCLACK_BASE_URL: clickclackBaseUrl,
        CLICKCLACK_BOT_TOKEN: created.bot_token.token,
        CLICKCLACK_WORKSPACE_ID: workspace.id,
        HERMES_CONNECTOR_ALLOWED_USER_IDS: owner.id,
        HERMES_CONNECTOR_ALLOWED_CHANNEL_IDS: channel.id,
        HERMES_CONNECTOR_CURSOR_FILE: join(temp, "connector-cursor.json"),
        HERMES_API_URL: `http://127.0.0.1:${hermesPort}`,
        HERMES_API_KEY: hermesSecret,
        HERMES_CONNECTOR_RECONNECT_MS: "50",
      },
    );
    await waitFor(
      async () => {
        throwIfSpawnFailed(connector!, "Hermes connector");
        return connector?.stdout.includes("initial event tail captured") ?? false;
      },
      15_000,
      "connector subscription",
    );

    const dm = await human.dms.create(workspace.id, [created.bot.id]);
    await human.dms.sendMessage(dm.id, { body: "first question", nonce: "e2e-human-1" });
    await waitForBotDm(human, dm.id, created.bot.id, "Hermes reply 1");

    await human.dms.sendMessage(dm.id, { body: "second question", nonce: "e2e-human-2" });
    await waitForBotDm(human, dm.id, created.bot.id, "Hermes reply 2");

    const root = await human.channels.sendMessage(channel.id, {
      body: "@hermes channel question",
      nonce: "e2e-human-channel",
    });
    await waitFor(
      async () => {
        const thread = await human.threads.get(root.id);
        return thread.replies.some(
          (message) => message.author_id === created.bot.id && message.body === "Hermes reply 3",
        );
      },
      15_000,
      "Hermes thread reply",
    );

    await waitFor(
      async () => observedEvents.some((event) => event.type === "agent.progress"),
      5_000,
      "agent progress event",
    );

    assert.equal(hermes.authFailures, 0, "connector made an unauthenticated Hermes request");
    assert.equal(hermes.runs.length, 3);
    assert.equal(hermes.runs[0]?.authorization, `Bearer ${hermesSecret}`);
    assert.match(hermes.runs[0]?.sessionKey ?? "", new RegExp(`^cc:${workspace.id}:dm:${dm.id}$`));
    assert.match(
      hermes.runs[2]?.sessionKey ?? "",
      new RegExp(`^cc:${workspace.id}:thread:${root.id}$`),
    );

    const secondHistory = hermes.runs[1]?.body.conversation_history;
    assert.ok(Array.isArray(secondHistory));
    assert.deepEqual(
      secondHistory.map((entry) => (entry as { role: string; content: string }).role),
      ["user", "assistant"],
    );
    assert.deepEqual(
      secondHistory.map((entry) => (entry as { role: string; content: string }).content),
      ["first question", "Hermes reply 1"],
    );

    const progressJson = JSON.stringify(
      observedEvents
        .filter((event) => event.type === "agent.progress")
        .map((event) => event.payload),
    );
    assert.match(progressJson, /web_search/);
    assert.doesNotMatch(progressJson, /private chain of thought|should-never-leak/);

    console.log(
      JSON.stringify({
        status: "ok",
        workspace: workspace.id,
        owner: owner.id,
        bot: created.bot.id,
        runs: hermes.runs.length,
        progressEvents: observedEvents.filter((event) => event.type === "agent.progress").length,
        dmContinuity: true,
        threadReply: true,
      }),
    );
  } catch (error) {
    if (connector) {
      console.error("connector stdout tail:\n" + connector.stdout.slice(-4_000));
      console.error("connector stderr tail:\n" + connector.stderr.slice(-4_000));
    }
    if (clickclack) {
      console.error("clickclack stdout tail:\n" + clickclack.stdout.slice(-4_000));
      console.error("clickclack stderr tail:\n" + clickclack.stderr.slice(-4_000));
    }
    throw error;
  } finally {
    observer?.close();
    if (connector) await stopManagedProcess(connector, "SIGTERM");
    if (hermes) await closeServer(hermes.server);
    if (clickclack) await stopManagedProcess(clickclack, "SIGINT");
    await rm(temp, { recursive: true, force: true });
  }
}

async function startClickClack(port: number, dataDirectory: string): Promise<ManagedProcess> {
  const binary = process.env.CLICKCLACK_E2E_BIN?.trim();
  const args = [
    "serve",
    `--addr=127.0.0.1:${port}`,
    `--data=${dataDirectory}`,
    "--dev-bootstrap=true",
  ];
  if (binary) return startManagedProcess(binary, args, repositoryRoot, process.env);

  const buildRoot = join(dataDirectory, "source");
  await mkdir(join(buildRoot, "apps"), { recursive: true });
  await Promise.all([
    copyFile(join(repositoryRoot, "go.mod"), join(buildRoot, "go.mod")),
    copyFile(join(repositoryRoot, "go.sum"), join(buildRoot, "go.sum")),
    cp(join(repositoryRoot, "apps/api"), join(buildRoot, "apps/api"), { recursive: true }),
  ]);
  await cp(
    join(repositoryRoot, "apps/web/dist"),
    join(buildRoot, "apps/api/internal/webassets/dist"),
    { recursive: true },
  );
  return startManagedProcess(
    "go",
    ["run", "./apps/api/cmd/clickclack", ...args],
    buildRoot,
    process.env,
  );
}

function startManagedProcess(
  command: string,
  args: string[],
  cwd: string,
  env: NodeJS.ProcessEnv,
): ManagedProcess {
  const child = spawn(command, args, {
    cwd,
    env,
    detached: process.platform !== "win32",
    stdio: ["pipe", "pipe", "pipe"],
  });
  const managed: ManagedProcess = { child, stdout: "", stderr: "" };
  child.once("error", (error) => {
    managed.spawnError = error;
    managed.stderr = appendBounded(managed.stderr, `${error.message}\n`);
  });
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk: string) => {
    managed.stdout = appendBounded(managed.stdout, chunk);
  });
  child.stderr.on("data", (chunk: string) => {
    managed.stderr = appendBounded(managed.stderr, chunk);
  });
  return managed;
}

function throwIfSpawnFailed(managed: ManagedProcess, label: string): void {
  if (managed.spawnError) {
    throw new Error(`${label} failed to start: ${managed.spawnError.message}`);
  }
}

async function stopManagedProcess(managed: ManagedProcess, signal: NodeJS.Signals): Promise<void> {
  if (managed.child.exitCode !== null || managed.child.signalCode !== null) return;
  const exited = new Promise<void>((resolve) => managed.child.once("exit", () => resolve()));
  try {
    if (process.platform !== "win32" && managed.child.pid) process.kill(-managed.child.pid, signal);
    else managed.child.kill(signal);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
  }
  if (await settlesWithin(exited, 5_000)) return;
  try {
    if (process.platform !== "win32" && managed.child.pid)
      process.kill(-managed.child.pid, "SIGKILL");
    else managed.child.kill("SIGKILL");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
  }
  await settlesWithin(exited, 2_000);
}

async function startMockHermes(port: number): Promise<{
  server: Server;
  runs: CapturedRun[];
  authFailures: number;
}> {
  const runs: CapturedRun[] = [];
  const state = { authFailures: 0 };
  const server = createHttpServer((request, response) => {
    void handleHermesRequest(request, response, runs, state).catch((error: unknown) => {
      response.writeHead(500, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({ error: error instanceof Error ? error.message : String(error) }),
      );
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => resolve());
  });
  return {
    server,
    runs,
    get authFailures() {
      return state.authFailures;
    },
  };
}

async function handleHermesRequest(
  request: IncomingMessage,
  response: ServerResponse,
  runs: CapturedRun[],
  state: { authFailures: number },
): Promise<void> {
  if (request.headers.authorization !== `Bearer ${hermesSecret}`) {
    state.authFailures += 1;
    response.writeHead(401, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ error: "unauthorized" }));
    return;
  }

  if (request.method === "GET" && request.url === "/v1/capabilities") {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        object: "hermes.capabilities",
        platform: "hermes-agent",
        model: "integration-mock",
        features: {
          run_submission: true,
          run_status: true,
          run_events_sse: true,
          run_stop: true,
        },
      }),
    );
    return;
  }

  if (request.method === "POST" && request.url === "/v1/runs") {
    const runId = `run_${runs.length + 1}`;
    runs.push({
      runId,
      authorization: request.headers.authorization,
      sessionKey: headerValue(request.headers["x-hermes-session-key"]),
      body: await readJsonObject(request),
    });
    response.writeHead(202, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ object: "hermes.run", run_id: runId, status: "queued" }));
    return;
  }

  const eventMatch = request.url?.match(/^\/v1\/runs\/(run_\d+)\/events$/u);
  if (request.method === "GET" && eventMatch) {
    const runId = eventMatch[1]!;
    const runNumber = Number(runId.slice("run_".length));
    response.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });
    writeSse(response, "reasoning.available", {
      event: "reasoning.available",
      run_id: runId,
      text: "private chain of thought",
    });
    writeSse(response, "tool.started", {
      event: "tool.started",
      run_id: runId,
      tool: "web_search",
      preview: "should-never-leak",
    });
    writeSse(response, "tool.completed", {
      event: "tool.completed",
      run_id: runId,
      tool: "web_search",
      error: false,
    });
    writeSse(response, "run.completed", {
      event: "run.completed",
      run_id: runId,
      output: `Hermes reply ${runNumber}`,
    });
    response.end();
    return;
  }

  if (request.method === "POST" && request.url?.match(/^\/v1\/runs\/run_\d+\/stop$/u)) {
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({ object: "hermes.run", status: "stopping" }));
    return;
  }

  response.writeHead(404, { "Content-Type": "application/json" });
  response.end(JSON.stringify({ error: "not found" }));
}

function writeSse(response: ServerResponse, event: string, data: Record<string, unknown>): void {
  response.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

async function readJsonObject(request: IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  let total = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    total += buffer.length;
    if (total > 1_048_576) throw new Error("request body too large");
    chunks.push(buffer);
  }
  const value = JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("expected JSON object");
  return value as Record<string, unknown>;
}

function headerValue(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

async function waitForBotDm(
  client: ClickClackClient,
  conversationId: string,
  botId: string,
  expectedBody: string,
): Promise<void> {
  await waitFor(
    async () => {
      const messages = await client.dms.messages(conversationId, 0, 100);
      return messages.some(
        (message) => message.author_id === botId && message.body === expectedBody,
      );
    },
    15_000,
    `DM reply ${expectedBody}`,
  );
}

async function waitForWebSocketOpen(socket: WebSocket): Promise<void> {
  if (socket.readyState === WebSocket.OPEN) return;
  await new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error("WebSocket open timed out")), 5_000);
    socket.addEventListener(
      "open",
      () => {
        clearTimeout(timeout);
        resolve();
      },
      { once: true },
    );
    socket.addEventListener(
      "error",
      () => {
        clearTimeout(timeout);
        reject(new Error("WebSocket failed before opening"));
      },
      { once: true },
    );
  });
}

async function waitFor(
  predicate: () => Promise<boolean>,
  timeoutMs: number,
  label: string,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      if (await predicate()) return;
      lastError = undefined;
    } catch (error) {
      lastError = error;
    }
    await delay(50);
  }
  const suffix = lastError instanceof Error ? `: ${lastError.message}` : "";
  throw new Error(`${label} timed out${suffix}`);
}

async function reservePort(): Promise<number> {
  const server = createNetServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve());
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  const port = address.port;
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return port;
}

async function closeServer(server: Server): Promise<void> {
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
}

async function settlesWithin(promise: Promise<void>, timeoutMs: number): Promise<boolean> {
  return Promise.race([promise.then(() => true), delay(timeoutMs).then(() => false)]);
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function appendBounded(current: string, chunk: string): string {
  return (current + chunk).slice(-64_000);
}

await main();
