import test from "node:test";
import assert from "node:assert/strict";
import { mergeMessageUpdate } from "./chat/messageUpdates.ts";

for (const [older, newer] of [
  ["2026-09-01T12:00:00Z", "2026-09-01T12:00:00.1Z"],
  ["2026-09-01T12:00:00.0000001Z", "2026-09-01T12:00:00.0000002Z"],
  ["2026-09-01T12:00:00.9Z", "2026-09-01T12:00:01Z"],
]) {
  test(`edits retain the newer body in either arrival order: ${older} / ${newer}`, () => {
    const first = { id: "message", body: "Earlier edit", edited_at: older };
    const second = { id: "message", body: "Later edit", edited_at: newer };
    for (const [current, incoming] of [
      [first, second],
      [second, first],
    ]) {
      const merged = mergeMessageUpdate(current, incoming);
      assert.equal(merged.body, second.body);
      assert.equal(merged.edited_at, newer);
    }
  });
}

test("a delayed edit cannot restore deleted content or attachments", () => {
  const deleted = {
    id: "message",
    body: "",
    deleted_at: "2026-09-01T12:00:01Z",
    attachments: [],
  };
  const earlier = {
    id: "message",
    body: "Deleted text",
    attachments: [
      {
        id: "upload",
        workspace_id: "workspace",
        owner_id: "author",
        filename: "file.txt",
        content_type: "text/plain",
        byte_size: 1,
        created_at: "2026-09-01T12:00:00Z",
      },
    ],
  };
  const merged = mergeMessageUpdate(deleted, earlier);
  assert.equal(merged.body, "");
  assert.equal(merged.deleted_at, deleted.deleted_at);
  assert.deepEqual(merged.attachments, []);
});
