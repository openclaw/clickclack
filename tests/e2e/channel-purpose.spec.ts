import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

test("channel purpose refreshes in main and embed headers", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Purpose Realtime ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };

  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: {
      name: `purpose-${suffix}`,
      description: "Coordinate the initial rollout",
      kind: "public",
    },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const mainPurpose = page.locator(".topbar .channel-purpose");
  await expect(mainPurpose).toContainText("Coordinate the initial rollout");

  const embedPage = await page.context().newPage();
  await embedPage.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);
  const embedPurpose = embedPage.locator(".embed-channel-header .channel-purpose");
  await expect(embedPurpose).toContainText("Coordinate the initial rollout");

  const updated = "Coordinate the reviewed rollout";
  const updateResponse = await page.request.patch(`/api/channels/${channel.id}`, {
    data: { description: updated },
  });
  expect(updateResponse.ok()).toBe(true);
  await expect(mainPurpose).toContainText(updated);
  await expect(embedPurpose).toContainText(updated);

  const clearResponse = await page.request.patch(`/api/channels/${channel.id}`, {
    data: { description: "" },
  });
  expect(clearResponse.ok()).toBe(true);
  await expect(mainPurpose).toHaveCount(0);
  await expect(embedPurpose).toHaveCount(0);
});
