import assert from "node:assert/strict";
import test from "node:test";

import { ClickClackClient } from "../src/index.ts";

const page = {
  messages: [{ id: "msg_1", body: "hello" }],
  oldest_seq: 11,
  newest_seq: 19,
  has_older: true,
  has_newer: false,
};

test("message page helpers preserve pagination metadata", async () => {
  const requests = [];
  const client = new ClickClackClient({
    baseUrl: "https://clickclack.example",
    fetch: async (input) => {
      requests.push(String(input));
      return Response.json(page);
    },
  });

  assert.deepEqual(await client.channels.messagesPage("chn_1", { beforeSeq: 20, limit: 10 }), page);
  assert.deepEqual(await client.channels.messages("chn_1", { beforeSeq: 20, limit: 10 }), [
    ...page.messages,
  ]);
  assert.deepEqual(await client.dms.messagesPage("dm_1", { aroundSeq: 15, limit: 9 }), page);
  assert.deepEqual(await client.dms.messages("dm_1", { aroundSeq: 15, limit: 9 }), [
    ...page.messages,
  ]);

  assert.deepEqual(requests, [
    "https://clickclack.example/api/channels/chn_1/messages?before_seq=20&limit=10",
    "https://clickclack.example/api/channels/chn_1/messages?before_seq=20&limit=10",
    "https://clickclack.example/api/dms/dm_1/messages?around_seq=15&limit=9",
    "https://clickclack.example/api/dms/dm_1/messages?around_seq=15&limit=9",
  ]);
});
