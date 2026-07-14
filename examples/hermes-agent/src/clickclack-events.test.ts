import assert from "node:assert/strict";
import test from "node:test";

import { ClickClackClient, type AgentProgressPayload } from "../../../packages/sdk-ts/src/index.ts";

test("events.list requests a bounded page from a captured cursor", async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  const client = new ClickClackClient({
    baseUrl: "https://clickclack.example/",
    token: "ccb_test",
    fetch: async (input, init) => {
      requests.push({ url: String(input), init });
      return Response.json({
        events: [
          {
            id: "evt_1",
            cursor: "cur_2",
            type: "message.created",
            workspace_id: "wsp_1",
            created_at: "2026-07-14T00:00:00Z",
            payload: {},
          },
        ],
        tail_cursor: "cur_tail",
      });
    },
  });

  const page = await client.events.list({
    workspaceId: "wsp 1",
    afterCursor: "cur+1",
    limit: 5,
    includeTail: true,
  });

  assert.equal(requests.length, 1);
  assert.equal(
    requests[0]?.url,
    "https://clickclack.example/api/realtime/events?workspace_id=wsp+1&after_cursor=cur%2B1&limit=5&include_tail=true",
  );
  assert.equal(new Headers(requests[0]?.init?.headers).get("Authorization"), "Bearer ccb_test");
  assert.equal(page.tailCursor, "cur_tail");
  assert.equal(page.events[0]?.cursor, "cur_2");
});

test("threads.get can request the latest bounded reply window", async () => {
  let requestUrl = "";
  const client = new ClickClackClient({
    baseUrl: "https://clickclack.example",
    fetch: async (input) => {
      requestUrl = String(input);
      return Response.json({ root: {}, replies: [] });
    },
  });

  await client.threads.get("msg_1", { limit: 200, latest: true });

  assert.equal(
    requestUrl,
    "https://clickclack.example/api/messages/msg_1/thread?limit=200&latest=true",
  );
});

test("events.publishEphemeral accepts a typed agent progress frame", async () => {
  let requestBody: unknown;
  const client = new ClickClackClient({
    baseUrl: "https://clickclack.example",
    token: "ccb_test",
    fetch: async (_input, init) => {
      requestBody = JSON.parse(String(init?.body));
      return Response.json({
        event: {
          id: "evt_progress",
          cursor: "",
          type: "agent.progress",
          workspace_id: "wsp_1",
          channel_id: "chn_1",
          created_at: "2026-07-14T00:00:00Z",
          payload: {},
        },
      });
    },
  });
  const payload: AgentProgressPayload = {
    turn_id: "msg_source",
    op: "append",
    line: { id: "lifecycle", kind: "lifecycle", text: "Working" },
  };

  await client.events.publishEphemeral({
    workspaceId: "wsp_1",
    channelId: "chn_1",
    type: "agent.progress",
    payload,
  });

  assert.deepEqual(requestBody, {
    workspace_id: "wsp_1",
    channel_id: "chn_1",
    type: "agent.progress",
    payload,
  });
});
