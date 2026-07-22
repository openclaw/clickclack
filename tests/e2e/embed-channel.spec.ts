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

  const uiEditedBody = `${initialBody} edited in embed`;
  await initialRow.hover();
  await initialRow.getByRole("button", { name: "Edit message" }).click();
  await initialRow.getByLabel("Edit message").fill(uiEditedBody);
  await initialRow.getByRole("button", { name: "Save" }).click();
  await expect(initialRow.locator(".markdown")).toContainText(uiEditedBody);
  await expect(initialRow.getByText("(edited)")).toBeVisible();

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

test("embedded channel fits narrow host panels without horizontal overflow", async ({ page }) => {
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
    channel: { id: string; route_id: string };
  };

  await page.setViewportSize({ width: 400, height: 700 });
  await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);

  await expect(page.locator(".empty")).toContainText("Welcome to");
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

    const shellChildBounds = await page
      .locator(".embed-channel-header, .messages, .embed-channel-composer-dock")
      .evaluateAll((elements) =>
        elements.map((element) => {
          const bounds = element.getBoundingClientRect();
          return { x: bounds.x, width: bounds.width };
        }),
      );
    expect(shellChildBounds).toHaveLength(3);
    for (const bounds of shellChildBounds) {
      expect(bounds.x).toBeGreaterThanOrEqual(0);
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    }

    for (const element of [
      page.getByRole("link", { name: "Open in ClickClack" }),
      page.locator(".send"),
    ]) {
      const bounds = await element.boundingBox();
      expect(bounds).not.toBeNull();
      expect(bounds!.x).toBeGreaterThanOrEqual(0);
      expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport.width);
    }

    const composerBounds = await page.locator(".embed-channel-composer").boundingBox();
    expect(composerBounds).not.toBeNull();
    expect(composerBounds!.x).toBeCloseTo(0, 1);
    expect(composerBounds!.width).toBeCloseTo(viewport.width, 1);
  };

  await expectPanelToFit({ width: 400, height: 700 });
  await expectPanelToFit({ width: 320, height: 600 });

  const longMessage = `sha256:${"a".repeat(64)}`;
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: longMessage },
  });
  expect(messageResponse.ok()).toBe(true);

  await page.reload();
  await expect(page.locator(".markdown").filter({ hasText: longMessage })).toBeVisible();
  await expectPanelToFit({ width: 320, height: 600 });
});
