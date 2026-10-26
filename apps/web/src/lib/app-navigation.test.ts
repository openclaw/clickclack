import assert from "node:assert/strict";
import { test } from "node:test";

import { createAppNavigator } from "./app-navigation.ts";

test("returning to the current path supersedes an unfinished navigation", async () => {
  let finish!: () => void;
  const unfinished = new Promise<void>((resolve) => {
    finish = resolve;
  });
  const paths: string[] = [];
  const navigate = createAppNavigator(
    () => "/channel",
    async (path) => {
      paths.push(path);
      if (path === "/thread") await unfinished;
    },
  );

  const opening = navigate("/thread");
  await navigate("/channel");
  finish();
  await opening;

  assert.deepEqual(paths, ["/thread", "/channel"]);
});

test("an idle current path stays a no-op after a navigation fails", async () => {
  const paths: string[] = [];
  const navigate = createAppNavigator(
    () => "/channel",
    async (path) => {
      paths.push(path);
      throw new Error("navigation failed");
    },
  );

  await navigate("/channel");
  await assert.rejects(navigate("/thread"), /navigation failed/);
  await navigate("/channel");

  assert.deepEqual(paths, ["/thread"]);
});
