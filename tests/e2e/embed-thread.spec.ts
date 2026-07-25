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
  const editedRoot = `${message.body} edited in embed`;
  await rootRow.getByRole("button", { name: "Edit message" }).click();
  await rootRow.getByLabel("Edit message").fill(editedRoot);
  await rootRow.getByRole("button", { name: "Save" }).click();
  await expect(rootRow.locator(".markdown")).toContainText(editedRoot);
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
  const { message: createdReply } = (await realtimeResponse.json()) as {
    message: { id: string };
  };
  const realtimeReplyRow = page.locator(`[data-message-id="${createdReply.id}"]`);
  await expect(realtimeReplyRow.locator(".markdown")).toContainText(realtimeReply);
  await expect(realtimeReplyRow.getByRole("button", { name: "Add reaction" })).toBeVisible();
  const replyReaction = await page.request.post(`/api/messages/${createdReply.id}/reactions`, {
    data: { emoji: "✅" },
  });
  expect(replyReaction.ok()).toBe(true);
  await expect(realtimeReplyRow.getByRole("button", { name: "✅ — 1 reaction" })).toBeVisible();

  const editedReply = `${realtimeReply} edited`;
  await realtimeReplyRow.getByRole("button", { name: "Edit message" }).click();
  await realtimeReplyRow.getByLabel("Edit message").fill(editedReply);
  await realtimeReplyRow.getByRole("button", { name: "Save" }).click();
  await expect(realtimeReplyRow.locator(".markdown")).toContainText(editedReply);
  await expect(realtimeReplyRow.getByRole("button", { name: "✅ — 1 reaction" })).toBeVisible();

  const deleteResponse = await page.request.delete(`/api/messages/${createdReply.id}`);
  expect(deleteResponse.ok()).toBe(true);
  await expect(page.locator(`[data-message-id="${createdReply.id}"] .message-deleted`)).toHaveText(
    "This message was deleted.",
  );
});

test("embedded thread fits narrow host panels without horizontal overflow", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const stamp = Date.now();

  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `s-8a4123d56515f4446b2cdef3f5693a66-${stamp}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string };
  };

  const rootBody = `narrow thread root sha256:${"a".repeat(64)}`;
  const rootResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: rootBody },
  });
  expect(rootResponse.ok()).toBe(true);
  const { message } = (await rootResponse.json()) as {
    message: { id: string };
  };

  const threadResponse = await page.request.get(`/api/messages/${message.id}/thread`);
  expect(threadResponse.ok()).toBe(true);
  const { root } = (await threadResponse.json()) as {
    root: { route_id: string };
  };

  const replyBody = `narrow thread reply sha256:${"b".repeat(64)}`;
  const replyResponse = await page.request.post(`/api/messages/${message.id}/thread/replies`, {
    data: { body: replyBody, nonce: `embed-narrow-reply-${stamp}` },
  });
  expect(replyResponse.ok()).toBe(true);

  await page.setViewportSize({ width: 400, height: 700 });
  await page.goto(`/embed/thread/${workspace.route_id}/${root.route_id}`);

  await expect(page.getByLabel("Embedded thread")).toBeVisible();
  await expect(page.locator(".thread-root .markdown")).toContainText(rootBody);
  // The overflow this guards against only manifests once the display webfont's
  // wider metrics are active, so settle fonts before measuring anything.
  await page.evaluate(() => document.fonts.ready);

  const expectPanelToFit = async (viewport: { width: number; height: number }) => {
    await page.setViewportSize(viewport);

    const scrollingBounds = await page.evaluate(() => {
      const scrollingElement = document.scrollingElement;
      if (!scrollingElement) throw new Error("Document has no scrolling element");
      return {
        scrollWidth: scrollingElement.scrollWidth,
        clientWidth: scrollingElement.clientWidth,
      };
    });
    expect(scrollingBounds.scrollWidth).toBeLessThanOrEqual(scrollingBounds.clientWidth);

    const threadChildBounds = await page
      .locator(".thread > header, .thread-scroll, .reply-composer")
      .evaluateAll((elements) =>
        elements.map((element) => {
          const bounds = element.getBoundingClientRect();
          return { x: bounds.x, width: bounds.width };
        }),
      );
    expect(threadChildBounds).toHaveLength(3);
    for (const bounds of threadChildBounds) {
      expect(bounds.x).toBeGreaterThanOrEqual(0);
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    }

    const sendButtonBounds = await page.locator(".reply-composer .send").boundingBox();
    expect(sendButtonBounds).not.toBeNull();
    expect(sendButtonBounds!.x).toBeGreaterThanOrEqual(0);
    expect(sendButtonBounds!.x + sendButtonBounds!.width).toBeLessThanOrEqual(viewport.width);
  };

  await expectPanelToFit({ width: 400, height: 700 });
  await expectPanelToFit({ width: 320, height: 600 });
});
