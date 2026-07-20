import { expect, test } from "@playwright/test";

test("embedded channel loads, sends idempotently, and follows realtime updates", async ({
  page,
}) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const stamp = Date.now();

  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `embed-channel-${stamp}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };

  const initialBody = `embedded channel root ${stamp}`;
  const initialResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: initialBody },
  });
  expect(initialResponse.ok()).toBe(true);
  const { message: initialMessage } = (await initialResponse.json()) as {
    message: { id: string };
  };

  await page.setViewportSize({ width: 800, height: 700 });
  await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);

  await expect(page.getByLabel("Embedded channel")).toBeVisible();
  const channelBounds = await page.getByLabel("Embedded channel").boundingBox();
  expect(channelBounds?.x).toBe(0);
  expect(channelBounds?.width).toBe(800);
  await expect(page.locator(".markdown").filter({ hasText: initialBody })).toBeVisible();
  await expect(page.getByRole("heading", { name: channel.name })).toBeVisible();
  const openLink = page.getByRole("link", { name: "Open in ClickClack" });
  await expect(openLink).toHaveAttribute("href", `/app/${workspace.route_id}/${channel.route_id}`);
  await expect(openLink).toHaveAttribute("target", "_blank");
  await expect(page.locator(".sidebar, .topbar, .guild-rail")).toHaveCount(0);

  const initialRow = page.locator(`[data-message-id="${initialMessage.id}"]`);
  const reactionResponse = await page.request.post(`/api/messages/${initialMessage.id}/reactions`, {
    data: { emoji: "👀" },
  });
  expect(reactionResponse.ok()).toBe(true);
  await expect(initialRow.getByRole("button", { name: "👀 — 1 reaction" })).toBeVisible();
  await initialRow.getByRole("button", { name: "👀 — 1 reaction" }).click();
  await expect(initialRow.getByRole("button", { name: "👀 — 1 reaction" })).toHaveCount(0);

  const composer = page.getByLabel("Message body");
  const uiBody = `embedded channel message ${stamp}`;
  await composer.fill(uiBody);
  const requestPromise = page.waitForRequest(
    (request) =>
      request.method() === "POST" && request.url().includes(`/api/channels/${channel.id}/messages`),
  );
  await page.getByRole("button", { name: "Send" }).click();
  const sendRequest = await requestPromise;
  const sendPayload = sendRequest.postDataJSON() as { body: string; nonce?: string };
  expect(sendPayload.body).toBe(uiBody);
  expect(sendPayload.nonce).toMatch(/^[a-zA-Z0-9]+$/);
  await expect(page.locator(".markdown").filter({ hasText: uiBody })).toBeVisible();

  const realtimeBody = `realtime channel message ${stamp}`;
  const realtimeResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: realtimeBody, nonce: `embed-channel-realtime-${stamp}` },
  });
  expect(realtimeResponse.ok()).toBe(true);
  await expect(page.locator(".markdown").filter({ hasText: realtimeBody })).toBeVisible();

  const { message: realtimeMessage } = (await realtimeResponse.json()) as {
    message: { id: string };
  };
  const editedBody = `${realtimeBody} edited`;
  const editResponse = await page.request.patch(`/api/messages/${realtimeMessage.id}`, {
    data: { body: editedBody },
  });
  expect(editResponse.ok()).toBe(true);
  await expect(page.locator(".markdown").filter({ hasText: editedBody })).toBeVisible();

  const deleteResponse = await page.request.delete(`/api/messages/${realtimeMessage.id}`);
  expect(deleteResponse.ok()).toBe(true);
  await expect(
    page.locator(`[data-message-id="${realtimeMessage.id}"] .message-deleted`),
  ).toHaveText("This message was deleted.");
});
