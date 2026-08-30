import { expect, test } from "@playwright/test";

test("embedded composer formats selections and inserts GIFs in a narrow panel", async ({
  page,
}, testInfo) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = await workspacesResponse.json();
  const workspace = workspaces[0];
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `embed-composer-${Date.now()}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = await channelResponse.json();
  await page.route("https://media.giphy.com/**", (route) =>
    route.fulfill({
      contentType: "image/gif",
      body: Buffer.from("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==", "base64"),
    }),
  );
  await page.setViewportSize({ width: 320, height: 600 });
  await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);
  const composer = page.getByLabel("Message body");
  const toggle = page.getByRole("button", { name: "GIF picker", exact: true });
  await composer.fill("ship it");
  await toggle.click();
  await page.screenshot({ path: testInfo.outputPath("composer-controls.png") });
  await toggle.click();

  for (const [button, wrapped] of [
    ["Bold", "**selected**"],
    ["Italic", "_selected_"],
    ["Code", "`selected`"],
    ["Code block", "\n```\nselected\n```\n"],
    ["Link", "[selected](https://)"],
  ]) {
    await composer.fill("before selected after");
    await composer.evaluate((node: HTMLTextAreaElement) => node.setSelectionRange(7, 15));
    await page.getByRole("button", { name: button, exact: true }).click();
    await expect(composer).toHaveValue(`before ${wrapped} after`);
  }

  await composer.fill("before after");
  await composer.evaluate((node: HTMLTextAreaElement) => node.setSelectionRange(7, 7));
  await page.getByRole("button", { name: "Bold", exact: true }).click();
  await expect(composer).toHaveValue("before **text**after");
  await composer.pressSequentially("new");
  await expect(composer).toHaveValue("before **new**after");

  await composer.fill("ship it");
  await toggle.click();
  const picker = page.getByRole("dialog", { name: "GIF picker panel" });
  await expect(picker).toBeVisible();
  const bounds = await picker.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(320);
  expect(bounds!.y).toBeGreaterThanOrEqual(0);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(600);
  await page.getByLabel("Search GIFs").fill("not a matching reaction");
  await expect(picker.getByText("No GIFs found")).toBeVisible();
  await page.getByLabel("Search GIFs").fill("ship");
  await page.getByLabel("Search GIFs").press("Enter");
  await expect(composer).toHaveValue("ship it");
  await picker.getByRole("button", { name: /Ship it/ }).click();
  await expect(picker).not.toBeVisible();
  await expect(composer).toHaveValue(/^ship it\n!\[Ship it\]\(https:\/\/media.giphy.com\/.+\)$/);
  await expect(composer).toBeFocused();
  await toggle.click();
  await page.getByLabel("Search GIFs").press("Escape");
  await expect(picker).not.toBeVisible();
  await expect(composer).toBeFocused();

  let releaseSend!: () => void;
  const sendGate = new Promise<void>((resolve) => {
    releaseSend = resolve;
  });
  await page.route(`**/api/channels/${channel.id}/messages`, async (route) => {
    if (route.request().method() === "POST") await sendGate;
    await route.continue();
  });
  await toggle.click();
  await page.getByRole("button", { name: "Send", exact: true }).click();
  try {
    await expect(composer).toBeDisabled();
    await expect(picker).not.toBeVisible();
    for (const button of await page.locator(".composer-toolbar button").all()) {
      await expect(button).toBeDisabled();
    }
  } finally {
    releaseSend();
  }
  await expect(composer).toHaveValue("");
  const sentGif = page.locator(".markdown").getByRole("img", { name: "Ship it" });
  await expect(sentGif).toBeVisible();
  await sentGif.click();
  const viewer = page.getByLabel("Image viewer");
  await expect(viewer.getByRole("img")).toBeVisible();
  await viewer.getByRole("button", { name: "Close image viewer" }).click();
  await expect(viewer).not.toBeVisible();
});

test("embedded channel loads, sends idempotently, and follows realtime updates", async ({
  page,
}) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const stamp = Date.now();
  const mentionHandle = `embed-channel-${stamp}`;
  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: "Embed Channel Mention",
      handle: mentionHandle,
      token_name: "e2e",
      scopes: ["bot:write"],
    },
  });
  expect(botResponse.ok()).toBe(true);

  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `embed-channel-${stamp}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };

  const initialBody = `embedded channel root ${stamp} for @${mentionHandle}`;
  const initialResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: initialBody },
  });
  expect(initialResponse.ok()).toBe(true);
  const { message: initialMessage } = (await initialResponse.json()) as {
    message: { id: string; author: { display_name: string } };
  };

  await page.setViewportSize({ width: 800, height: 700 });
  await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);

  await expect(page.getByLabel("Embedded channel")).toBeVisible();
  const channelBounds = await page.getByLabel("Embedded channel").boundingBox();
  expect(channelBounds?.x).toBe(0);
  expect(channelBounds?.width).toBe(800);
  await expect(page.locator(".markdown").filter({ hasText: initialBody })).toBeVisible();
  await expect(page.getByRole("heading", { name: channel.name })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open in ClickClack" })).toHaveCount(0);
  await expect(page.locator(".sidebar, .topbar, .guild-rail")).toHaveCount(0);

  const initialRow = page.locator(`[data-message-id="${initialMessage.id}"]`);
  const authorGroup = page.locator(".message-group").filter({ has: initialRow });
  await authorGroup
    .getByRole("button", { name: `View profile for ${initialMessage.author.display_name}` })
    .click();
  const profile = page.getByRole("dialog", { name: "Profile", exact: true });
  await expect(
    profile.getByRole("heading", { name: initialMessage.author.display_name }),
  ).toBeVisible();
  await profile.getByRole("button", { name: "Close profile" }).click();
  await expect(profile).not.toBeVisible();
  await expect(initialRow.locator("mark[data-clickclack-mention='true']")).toHaveText(
    `@${mentionHandle}`,
  );
  const reactionResponse = await page.request.post(`/api/messages/${initialMessage.id}/reactions`, {
    data: { emoji: "👀" },
  });
  expect(reactionResponse.ok()).toBe(true);
  await expect(initialRow.getByRole("button", { name: "👀 — 1 reaction" })).toBeVisible();
  await initialRow.getByRole("button", { name: "👀 — 1 reaction" }).click();
  await expect(initialRow.getByRole("button", { name: "👀 — 1 reaction" })).toHaveCount(0);

  const uiEditedBody = `${initialBody} edited in embed`;
  await initialRow.hover();
  await initialRow.getByRole("button", { name: "More actions" }).click();
  await initialRow.getByRole("menuitem", { name: "Edit message" }).click();
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

    const sendButtonBounds = await page.locator(".send").boundingBox();
    expect(sendButtonBounds).not.toBeNull();
    expect(sendButtonBounds!.x).toBeGreaterThanOrEqual(0);
    expect(sendButtonBounds!.x + sendButtonBounds!.width).toBeLessThanOrEqual(viewport.width);

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
