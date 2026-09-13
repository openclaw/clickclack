import { expect, test } from "@playwright/test";
import { readdirSync } from "node:fs";

// Exercise the production worker emitted by the normal build, including postprocessing.
test("built syntax worker preserves whitespace in its PHP grammar", async ({ page }) => {
  const filename = readdirSync("apps/api/internal/webassets/dist/_app/immutable/workers").find(
    (name) => name.startsWith("highlight.worker-") && name.endsWith(".js"),
  );
  expect(filename).toBeTruthy();
  await page.goto("/app");
  const result = await page.evaluate(async (filename) => {
    const worker = new Worker(`/_app/immutable/workers/${filename}`, { type: "module" });
    try {
      return await new Promise<{ html?: string; error?: string }>((resolve, reject) => {
        worker.onmessage = (event) => resolve(event.data);
        worker.onerror = () => reject(new Error("Built syntax worker failed"));
        worker.postMessage({
          source: "<?php $x = new Widget(); widget ();",
          language: "php",
          outputLimit: 10000,
        });
      });
    } finally {
      worker.terminate();
    }
  }, filename);
  expect(result.error).toBeUndefined();
  expect(result.html).toContain('<span class="hljs-title class_">Widget</span>');
  expect(result.html).toContain('<span class="hljs-title function_ invoke__">widget</span>');
});
