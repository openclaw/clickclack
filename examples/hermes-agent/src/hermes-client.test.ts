import assert from "node:assert/strict";
import test from "node:test";

import { HermesClient, HermesProtocolError } from "./hermes-client.ts";

function jsonResponse(value: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(value), {
    status: init.status ?? 200,
    headers: { "content-type": "application/json", ...init.headers },
  });
}

function sseResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(stream, { headers: { "content-type": "text/event-stream" } });
}

test("capability preflight requires the Hermes Runs contract", async () => {
  const requests: Array<{ url: string; headers: Headers }> = [];
  const client = new HermesClient({
    baseUrl: "https://hermes.example/",
    apiKey: "secret-key",
    fetch: async (input, init) => {
      requests.push({ url: String(input), headers: new Headers(init?.headers) });
      return jsonResponse({
        object: "hermes.api_server.capabilities",
        platform: "hermes-agent",
        model: "test-model",
        features: {
          run_submission: true,
          run_status: true,
          run_events_sse: true,
          run_stop: true,
          tool_progress_events: true,
          approval_events: true,
        },
      });
    },
  });

  const capabilities = await client.assertCompatible();

  assert.equal(capabilities.model, "test-model");
  assert.equal(requests[0]?.url, "https://hermes.example/v1/capabilities");
  assert.equal(requests[0]?.headers.get("Authorization"), "Bearer secret-key");
});

test("capability preflight rejects older Hermes servers", async () => {
  const client = new HermesClient({
    baseUrl: "https://hermes.example",
    fetch: async () =>
      jsonResponse({ platform: "hermes-agent", features: { run_submission: true } }),
  });

  await assert.rejects(() => client.assertCompatible(), /run_events_sse/);
});

test("startRun sends bounded history and stable session identity", async () => {
  let body: Record<string, unknown> | undefined;
  let headers = new Headers();
  const client = new HermesClient({
    baseUrl: "https://hermes.example",
    apiKey: "secret-key",
    fetch: async (_input, init) => {
      body = JSON.parse(String(init?.body)) as Record<string, unknown>;
      headers = new Headers(init?.headers);
      return jsonResponse(
        { object: "hermes.run", run_id: "run_123", status: "queued", session_id: "cc:wsp:dm:1" },
        { status: 202 },
      );
    },
  });

  const run = await client.startRun({
    input: "latest question",
    conversationHistory: [
      { role: "user", content: "old" },
      { role: "assistant", content: "answer" },
    ],
    instructions: "Reply for ClickClack.",
    sessionId: "cc:wsp:dm:1",
    sessionKey: "cc:wsp:dm:1",
  });

  assert.equal(run.runId, "run_123");
  assert.equal(headers.get("Authorization"), "Bearer secret-key");
  assert.equal(headers.get("X-Hermes-Session-Key"), "cc:wsp:dm:1");
  assert.deepEqual(body, {
    input: "latest question",
    conversation_history: [
      { role: "user", content: "old" },
      { role: "assistant", content: "answer" },
    ],
    instructions: "Reply for ClickClack.",
    session_id: "cc:wsp:dm:1",
  });
});

test("streamRunEvents parses split SSE frames and terminal output", async () => {
  const client = new HermesClient({
    baseUrl: "https://hermes.example",
    fetch: async () =>
      sseResponse([
        'event: tool.started\r\ndata: {"event":"tool.started","run_id":"run_1","tool":"web_search"}\r\n',
        '\r\nevent: message.delta\ndata: {"event":"message.delta","run_id":"run_1","delta":"Hel"}\n\n',
        'event: run.completed\ndata: {"event":"run.completed","run_id":"run_1","output":"Hello"}\n\n',
      ]),
  });

  const events = [];
  for await (const event of client.streamRunEvents("run_1")) events.push(event);

  assert.deepEqual(
    events.map((event) => event.event),
    ["tool.started", "message.delta", "run.completed"],
  );
  assert.equal(events[2]?.output, "Hello");
});

test("streamRunEvents accepts multiple bounded frames delivered in one large chunk", async () => {
  const frames = [
    `event: run.started\ndata: ${JSON.stringify({ event: "run.started", run_id: "run_1" })}\n\n`,
    `event: run.completed\ndata: ${JSON.stringify({ event: "run.completed", run_id: "run_1", output: "done" })}\n\n`,
  ].join("");
  const client = new HermesClient({
    baseUrl: "https://hermes.example",
    maxEventBytes: 100,
    fetch: async () => new Response(frames, { headers: { "Content-Type": "text/event-stream" } }),
  });

  const events = [];
  for await (const event of client.streamRunEvents("run_1")) events.push(event.event);
  assert.deepEqual(events, ["run.started", "run.completed"]);
});

test("streamRunEvents rejects non-SSE responses with a bounded error", async () => {
  const client = new HermesClient({
    baseUrl: "https://hermes.example",
    maxErrorBytes: 16,
    fetch: async () =>
      new Response("x".repeat(200), { status: 502, headers: { "content-type": "text/plain" } }),
  });

  await assert.rejects(
    async () => {
      for await (const _event of client.streamRunEvents("run_1")) {
        // consume
      }
    },
    (error: unknown) => {
      assert.ok(error instanceof HermesProtocolError);
      assert.match(error.message, /xxxxxxxxxxxxxxxx/);
      assert.doesNotMatch(error.message, /x{17}/);
      return true;
    },
  );
});

test("stopRun posts to the run stop endpoint", async () => {
  let request: { url: string; method?: string } | undefined;
  const client = new HermesClient({
    baseUrl: "https://hermes.example",
    fetch: async (input, init) => {
      request = { url: String(input), method: init?.method };
      return jsonResponse({ object: "hermes.run", run_id: "run_1", status: "stopping" });
    },
  });

  await client.stopRun("run_1");
  assert.deepEqual(request, { url: "https://hermes.example/v1/runs/run_1/stop", method: "POST" });
});
