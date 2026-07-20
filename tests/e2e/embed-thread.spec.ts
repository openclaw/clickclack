import { expect, test } from "@playwright/test";

test("embedded thread loads, posts replies, and follows realtime updates", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const stamp = Date.now();

  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `embed-${stamp}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };

  const rootResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: `embedded root ${stamp}` },
  });
  const { message } = (await rootResponse.json()) as {
    message: { id: string; body: string };
  };
  const threadResponse = await page.request.get(`/api/messages/${message.id}/thread`);
  const { root } = (await threadResponse.json()) as {
    root: { route_id: string };
  };

  await page.setViewportSize({ width: 800, height: 700 });
  await page.goto(`/embed/thread/${workspace.route_id}/${root.route_id}`);

  await expect(page.getByLabel("Embedded thread")).toBeVisible();
  const threadBounds = await page.getByLabel("Embedded thread").boundingBox();
  expect(threadBounds?.x).toBe(0);
  expect(threadBounds?.width).toBe(800);
  await expect(page.locator(".thread-root .markdown")).toContainText(message.body);
  await expect(page.getByText(`#${channel.name}`, { exact: true })).toBeVisible();
  const openLink = page.getByRole("link", { name: "Open in ClickClack" });
  await expect(openLink).toHaveAttribute("href", `/app/${workspace.route_id}/${root.route_id}`);
  await expect(openLink).toHaveAttribute("target", "_blank");
  await expect(page.locator(".sidebar, .topbar")).toHaveCount(0);

  const rootRow = page.locator(`[data-message-id="${message.id}"]`);
  await expect(rootRow.getByRole("button", { name: "Add reaction" })).toBeVisible();
  const rootReaction = await page.request.post(`/api/messages/${message.id}/reactions`, {
    data: { emoji: "🚀" },
  });
  expect(rootReaction.ok()).toBe(true);
  await expect(rootRow.getByRole("button", { name: "🚀 — 1 reaction" })).toBeVisible();

  const composer = page.getByLabel("Reply body");
  await composer.fill(`embedded reply ${stamp}`);
  await page.locator(".reply-composer").getByRole("button", { name: "Reply" }).click();
  await expect(
    page.locator(".reply .markdown").filter({ hasText: `embedded reply ${stamp}` }),
  ).toBeVisible();

  const realtimeReply = `realtime embed reply ${stamp}`;
  const realtimeResponse = await page.request.post(`/api/messages/${message.id}/thread/replies`, {
    data: { body: realtimeReply, nonce: `embed-realtime-${stamp}` },
  });
  expect(realtimeResponse.ok()).toBe(true);
  const realtimeReplyRow = page.locator(".reply").filter({ hasText: realtimeReply });
  await expect(realtimeReplyRow.locator(".markdown")).toBeVisible();

  const { message: createdReply } = (await realtimeResponse.json()) as {
    message: { id: string };
  };
  await expect(realtimeReplyRow.getByRole("button", { name: "Add reaction" })).toBeVisible();
  const replyReaction = await page.request.post(`/api/messages/${createdReply.id}/reactions`, {
    data: { emoji: "✅" },
  });
  expect(replyReaction.ok()).toBe(true);
  await expect(realtimeReplyRow.getByRole("button", { name: "✅ — 1 reaction" })).toBeVisible();

  const editedReply = `${realtimeReply} edited`;
  const editResponse = await page.request.patch(`/api/messages/${createdReply.id}`, {
    data: { body: editedReply },
  });
  expect(editResponse.ok()).toBe(true);
  await expect(page.locator(".reply .markdown").filter({ hasText: editedReply })).toBeVisible();

  const deleteResponse = await page.request.delete(`/api/messages/${createdReply.id}`);
  expect(deleteResponse.ok()).toBe(true);
  await expect(page.locator(`[data-message-id="${createdReply.id}"] .message-deleted`)).toHaveText(
    "This message was deleted.",
  );
});
