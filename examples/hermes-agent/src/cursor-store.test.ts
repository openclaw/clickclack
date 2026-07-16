import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { FileCursorStore } from "./cursor-store.ts";

test("FileCursorStore atomically persists a cursor across instances", async () => {
  const directory = await mkdtemp(join(tmpdir(), "clickclack-hermes-cursor-"));
  const path = join(directory, "state", "cursor.json");
  try {
    await new FileCursorStore(path, "wsp_1").save("cur_42");

    assert.equal(await new FileCursorStore(path, "wsp_1").load(), "cur_42");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("FileCursorStore fails closed when state belongs to another workspace", async () => {
  const directory = await mkdtemp(join(tmpdir(), "clickclack-hermes-cursor-"));
  const path = join(directory, "cursor.json");
  try {
    await new FileCursorStore(path, "wsp_1").save("cur_42");

    await assert.rejects(() => new FileCursorStore(path, "wsp_2").load(), /workspace/i);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
