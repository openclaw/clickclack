import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

test("text after an empty nested Markdown quote stays visible", async ({ page }) => {
  const suffix = randomUUID().slice(0, 8);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Markdown proof ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `rendering-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "**Markdown rendering**\n\n>>\nText after the empty nested quote." },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = (await messageResponse.json()) as { message: { id: string } };
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const row = page.locator(`.message-row[data-message-id="${message.id}"]`);
  const markdown = row.locator(".markdown");
  await expect(markdown.locator("strong")).toHaveText("Markdown rendering");
  if (process.env.PROOF_OUT) {
    await row.screenshot({
      path: `${process.env.PROOF_OUT}/markdown-${process.env.PROOF_TAG}.png`,
    });
  }
  await expect(markdown).toContainText("Text after the empty nested quote.");
});
