import { expect, test, type Locator, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

function clickclack(args: string[]): string {
  return execFileSync("go", ["run", "./apps/api/cmd/clickclack", ...args], {
    cwd: process.cwd(),
    encoding: "utf8",
  }).trim();
}

// Timeline rows expose editing through the ⋮ overflow menu.
async function openTimelineEditor(row: Locator) {
  await row.hover();
  await row.getByRole("button", { name: "More actions" }).click();
  await row.getByRole("menuitem", { name: "Edit message" }).click();
}

async function createOwnedMessage(page: Page, label: string) {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `${label} ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `${label.toLowerCase().replaceAll(" ", "-")}-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { route_id: string };
  };

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const body = `${label} ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  return { body, row };
}

test("message editing leaves composing shortcuts with the input method", async ({ page }) => {
  const { body, row: sentRow } = await createOwnedMessage(page, "Composing edit");
  const messageID = await sentRow.getAttribute("data-message-id");
  const row = page.locator(`.message-row[data-message-id="${messageID}"]`);
  await openTimelineEditor(row);
  const editor = row.getByLabel("Edit message");
  const draft = `${body} unfinished`;
  await editor.fill(draft);
  let patches = 0;
  page.on("request", (request) => {
    if (request.method() === "PATCH" && request.url().endsWith(`/api/messages/${messageID}`)) {
      patches += 1;
    }
  });
  for (const composing of [{ isComposing: true }, { keyCode: 229 }]) {
    for (const shortcut of [
      { key: "Escape" },
      { key: "Enter", ctrlKey: true },
      { key: "Enter", metaKey: true },
    ]) {
      await editor.dispatchEvent("keydown", { ...shortcut, ...composing });
      await expect(editor).toHaveValue(draft);
    }
  }
  const response = await page.request.get(`/api/messages/${messageID}`);
  expect(response.ok()).toBe(true);
  expect((await response.json()).message.body).toBe(body);
  expect(patches).toBe(0);

  await editor.press("Meta+Enter");
  await expect(row.locator(".markdown")).toHaveText(draft);
  await openTimelineEditor(row);
  await editor.fill("Discarded edit");
  await editor.press("Escape");
  await expect(editor).not.toBeVisible();
  await expect(row.locator(".markdown")).toHaveText(draft);
});

test("message action menu supports standard keyboard navigation", async ({ page }) => {
  const { row } = await createOwnedMessage(page, "Keyboard menu");
  const trigger = row.getByRole("button", { name: "More actions" });
  await row.hover();
  await trigger.click();

  const copy = row.getByRole("menuitem", { name: "Copy text" });
  const copyLink = row.getByRole("menuitem", { name: "Copy link" });
  const pin = row.getByRole("menuitem", { name: "Pin message" });
  const edit = row.getByRole("menuitem", { name: "Edit message" });
  const remove = row.getByRole("menuitem", { name: "Delete message" });
  await expect(copy).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(copyLink).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(pin).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(edit).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(remove).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(copy).toBeFocused();
  await page.keyboard.press("ArrowUp");
  await expect(remove).toBeFocused();
  await page.keyboard.press("Home");
  await expect(copy).toBeFocused();
  await page.keyboard.press("End");
  await expect(remove).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
  await expect(row.getByRole("menu", { name: "More actions" })).toHaveCount(0);

  await trigger.click();
  await expect(copy).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(row.getByRole("menu", { name: "More actions" })).toHaveCount(0);
});

test("copy message text reports success and failure", async ({ page }) => {
  const { body, row } = await createOwnedMessage(page, "Clipboard feedback");
  const pageErrors: Error[] = [];
  page.on("pageerror", (error) => pageErrors.push(error));
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          Object.assign(window, { copiedMessageText: value });
        },
      },
    });
  });

  const trigger = row.getByRole("button", { name: "More actions" });
  await row.hover();
  await trigger.click();
  await row.getByRole("menuitem", { name: "Copy text" }).click();
  await expect(row.locator(".message-copy-status")).toHaveText("Copied");
  await expect.poll(() => page.evaluate(() => Reflect.get(window, "copiedMessageText"))).toBe(body);

  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error("clipboard denied");
        },
      },
    });
  });
  await row.hover();
  await trigger.click();
  await row.getByRole("menuitem", { name: "Copy text" }).click();
  await expect(row.locator(".message-copy-status")).toHaveText("Couldn't copy");
  expect(pageErrors).toEqual([]);
});

test("message edits persist in channels and threads", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Editing Proof ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `editing-proof-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { route_id: string; name: string };
  };

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();

  const originalBody = `Original channel message ${suffix}`;
  const editedBody = `Edited channel message ${suffix}

| State | Value |
| --- | --- |
| Edit | preserved |`;
  await page.getByLabel("Message body").fill(originalBody);
  await page.getByRole("button", { name: "Send" }).click();
  let channelRow = page.locator(".message-row:not(.is-pending)", { hasText: originalBody });
  await expect(channelRow).toBeVisible();
  const channelMessageID = await channelRow.getAttribute("data-message-id");
  expect(channelMessageID).toBeTruthy();
  channelRow = page.locator(`.message-row[data-message-id="${channelMessageID}"]`);
  const reactionResponse = await page.request.post(`/api/messages/${channelMessageID}/reactions`, {
    data: { emoji: "✅" },
  });
  expect(reactionResponse.ok()).toBe(true);
  await expect(channelRow.getByRole("button", { name: "✅ — 1 reaction" })).toBeVisible();
  await openTimelineEditor(channelRow);
  const channelEditor = channelRow.getByLabel("Edit message");
  await expect(channelEditor).toBeFocused();
  await expect(channelEditor).toHaveValue(originalBody);
  const editorStyle = await channelEditor.evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      backgroundColor: style.backgroundColor,
      borderRadius: style.borderRadius,
      borderWidth: style.borderTopWidth,
    };
  });
  expect(editorStyle.backgroundColor).not.toBe("rgba(0, 0, 0, 0)");
  expect(editorStyle.borderRadius).not.toBe("0px");
  expect(editorStyle.borderWidth).not.toBe("0px");
  const saveStyle = await channelRow.getByRole("button", { name: "Save" }).evaluate((element) => {
    const style = getComputedStyle(element);
    return { backgroundColor: style.backgroundColor, color: style.color };
  });
  expect(saveStyle.color).not.toBe(saveStyle.backgroundColor);
  await channelEditor.fill(editedBody);
  await channelRow.getByRole("button", { name: "Save" }).click();
  await expect(channelRow.locator(".markdown")).toContainText(`Edited channel message ${suffix}`);
  await expect(channelRow.locator(".markdown table")).toContainText("preserved");
  await expect(channelRow.getByText("(edited)")).toBeVisible();
  await expect(channelRow.getByRole("button", { name: "✅ — 1 reaction" })).toBeVisible();

  await page.reload();
  await waitForAppReady(page);
  channelRow = page.locator(`.message-row[data-message-id="${channelMessageID}"]`);
  await expect(channelRow.locator(".markdown")).toContainText(`Edited channel message ${suffix}`);
  await expect(channelRow.locator(".markdown table")).toContainText("preserved");
  await expect(channelRow.getByText("(edited)")).toBeVisible();
  await expect(channelRow.getByRole("button", { name: "✅ — 1 reaction" })).toBeVisible();

  await channelRow.hover();
  await channelRow.getByRole("button", { name: "Open thread" }).click();
  const threadPane = page.getByLabel("Thread pane", { exact: true });
  await expect(threadPane).toBeVisible();
  await channelRow.hover();
  await channelRow.getByRole("button", { name: "More actions" }).focus();
  await expect(channelRow.locator(".message-actions")).toHaveCSS("opacity", "1");
  await channelRow.getByRole("button", { name: "More actions" }).click();
  await channelRow.getByRole("menuitem", { name: "Edit message" }).click();
  await expect(page.locator('textarea[aria-label="Edit message"]')).toHaveCount(1);
  await expect(channelRow.getByLabel("Edit message")).toBeFocused();
  await channelRow.getByLabel("Edit message").press("Escape");
  await expect(channelRow.getByRole("button", { name: "More actions" })).toBeFocused();
  const threadRoot = threadPane.locator(`.thread-root[data-message-id="${channelMessageID}"]`);
  await threadRoot.hover();
  await threadRoot.getByRole("button", { name: "Edit message" }).click();
  await expect(page.locator('textarea[aria-label="Edit message"]')).toHaveCount(1);
  await expect(threadRoot.getByLabel("Edit message")).toBeFocused();
  await expect(channelRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  await threadRoot.getByLabel("Edit message").press("Escape");
  await expect(threadRoot.getByRole("button", { name: "Edit message" })).toBeFocused();
  await threadRoot.getByRole("button", { name: "Edit message" }).click();
  await threadRoot.getByLabel("Edit message").fill("Discarded thread-root draft");
  await threadPane.getByRole("button", { name: "Close thread" }).click();
  await expect(threadRoot).not.toBeVisible();
  await channelRow.hover();
  await channelRow.getByRole("button", { name: "Open thread" }).click();
  await expect(threadPane).toBeVisible();
  await expect(threadPane.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  const reopenedThreadRoot = threadPane.locator(
    `.thread-root[data-message-id="${channelMessageID}"]`,
  );
  await expect(reopenedThreadRoot.locator(".markdown")).toContainText(
    `Edited channel message ${suffix}`,
  );
  await expect(reopenedThreadRoot.locator(".markdown table")).toContainText("preserved");
  await threadPane.getByRole("button", { name: "Close thread" }).click();
  await openTimelineEditor(channelRow);
  await expect(channelRow.getByLabel("Edit message")).toBeFocused();
  await channelRow.getByLabel("Edit message").press("Escape");
  await channelRow.hover();
  await channelRow.getByRole("button", { name: "Open thread" }).click();
  await expect(threadPane).toBeVisible();
  const originalReply = `Original thread reply ${suffix}`;
  const editedReply = `Edited thread reply ${suffix}`;
  await threadPane.getByLabel("Reply body").fill(originalReply);
  const persistedReply = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/messages/${channelMessageID}/thread/replies`),
  );
  await threadPane.locator(".reply-composer").getByRole("button", { name: "Reply" }).click();
  expect((await persistedReply).ok()).toBe(true);
  let reply = threadPane.locator(".reply", { hasText: originalReply });
  await expect(reply).toBeVisible();
  const replyMessageID = await reply.getAttribute("data-message-id");
  expect(replyMessageID).toBeTruthy();
  reply = threadPane.locator(`[data-message-id="${replyMessageID}"]`);

  await reply.hover();
  await reply.getByRole("button", { name: "Edit message" }).click();
  const replyEditor = reply.getByLabel("Edit message");
  await expect(replyEditor).toBeFocused();
  await replyEditor.fill(editedReply);
  if (process.env.MESSAGE_EDITING_EDITOR_PROOF_PATH) {
    await page.screenshot({
      path: process.env.MESSAGE_EDITING_EDITOR_PROOF_PATH,
      fullPage: true,
    });
  }
  await replyEditor.press("Control+Enter");
  await expect(reply.locator(".markdown")).toContainText(editedReply);
  await expect(reply.getByText("(edited)")).toBeVisible();

  if (process.env.MESSAGE_EDITING_PROOF_PATH) {
    await page.screenshot({ path: process.env.MESSAGE_EDITING_PROOF_PATH, fullPage: true });
  }

  await page.reload();
  await waitForAppReady(page);
  await expect(page.getByLabel("Thread pane", { exact: true })).toBeVisible();
  await expect(page.locator(".reply", { hasText: editedReply })).toBeVisible();
});

test("message edits submit boundary whitespace to server normalization", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Editing Whitespace ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `editing-whitespace-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: `Original whitespace ${suffix}` },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = (await messageResponse.json()) as { message: { id: string } };

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const row = page.locator(`[data-message-id="${message.id}"]`);
  await openTimelineEditor(row);
  const whitespaceBody = `    indented code ${suffix}\n`;
  await row.getByLabel("Edit message").fill(whitespaceBody);
  const submittedEdit = page.waitForResponse(
    (response) =>
      response.request().method() === "PATCH" &&
      response.url().endsWith(`/api/messages/${message.id}`),
  );
  await row.getByRole("button", { name: "Save" }).click();
  const editResponse = await submittedEdit;
  expect(editResponse.ok()).toBe(true);
  expect(editResponse.request().postDataJSON()).toEqual({ body: whitespaceBody });

  const persistedResponse = await page.request.get(`/api/messages/${message.id}`);
  expect(persistedResponse.ok()).toBe(true);
  const persisted = (await persistedResponse.json()) as { message: { body: string } };
  expect(persisted.message.body).toBe(whitespaceBody.trim());
});

test("edit sessions reject empty shortcuts and keep save failures visible", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Editing Race ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `editing-race-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  const alternateChannelResponse = await page.request.post(
    `/api/workspaces/${workspace.id}/channels`,
    { data: { name: `editing-alternate-${suffix}`, kind: "public" } },
  );
  expect(alternateChannelResponse.ok()).toBe(true);
  const { channel: alternateChannel } = (await alternateChannelResponse.json()) as {
    channel: { name: string; route_id: string };
  };
  const firstBody = `First edit ${suffix}`;
  const secondBody = `Second edit ${suffix}`;
  const firstResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: firstBody },
  });
  const secondResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: secondBody },
  });
  expect(firstResponse.ok()).toBe(true);
  expect(secondResponse.ok()).toBe(true);
  const { message: firstMessage } = (await firstResponse.json()) as {
    message: { id: string };
  };
  const { message: secondMessage } = (await secondResponse.json()) as {
    message: { id: string };
  };

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const firstRow = page.locator(`main.timeline [data-message-id="${firstMessage.id}"]`);
  const secondRow = page.locator(`main.timeline [data-message-id="${secondMessage.id}"]`);

  await openTimelineEditor(firstRow);
  const unsavedDraft = `Unsaved first edit ${suffix}`;
  await firstRow.getByLabel("Edit message").fill(unsavedDraft);
  const alternateRoute = `/api/routes/${workspace.route_id}/${alternateChannel.route_id}`;
  const routeEntered = deferred(),
    routeRelease = deferred(),
    routeDelivered = deferred();
  await page.route(
    `**${alternateRoute}`,
    async (route) => {
      const response = await route.fetch();
      routeEntered.resolve();
      await routeRelease.promise;
      await route.fulfill({ response });
      routeDelivered.resolve();
    },
    { times: 1 },
  );
  try {
    await page.getByRole("link", { name: `# ${alternateChannel.name}` }).click();
    await routeEntered.promise;
    await expect(page).toHaveURL(new RegExp(`/${alternateChannel.route_id}$`));
    await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
    await openTimelineEditor(secondRow);
    await expect(firstRow.getByLabel("Edit message")).toHaveValue(unsavedDraft);
    await expect(firstRow.getByLabel("Edit message")).toBeFocused();
    await expect(page).toHaveURL(new RegExp(`/${channel.route_id}$`));
  } finally {
    routeRelease.resolve();
    await routeDelivered.promise;
  }
  await page.getByRole("link", { name: `# ${alternateChannel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${alternateChannel.name}` })).toBeVisible();
  await expect(page.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await expect(firstRow.getByLabel("Edit message")).toHaveValue(unsavedDraft);
  const threadRouteResponse = await page.request.post(`/api/messages/${secondMessage.id}/route`);
  expect(threadRouteResponse.ok()).toBe(true);
  const { message: routedMessage } = (await threadRouteResponse.json()) as {
    message: { route_id: string };
  };
  const threadEntered = deferred(),
    threadRelease = deferred(),
    threadDelivered = deferred();
  await page.route(
    `**/api/routes/${workspace.route_id}/${routedMessage.route_id}`,
    async (route) => {
      const response = await route.fetch();
      threadEntered.resolve();
      await threadRelease.promise;
      await route.fulfill({ response });
      threadDelivered.resolve();
    },
    { times: 1 },
  );
  const threadPane = page.getByLabel("Thread pane", { exact: true });
  try {
    await secondRow.hover();
    await secondRow.getByRole("button", { name: "Open thread" }).click();
    await threadEntered.promise;
    await expect(threadPane.getByRole("button", { name: "Close thread" })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`/${routedMessage.route_id}$`));
    await openTimelineEditor(secondRow);
    await expect(firstRow.getByLabel("Edit message")).toHaveValue(unsavedDraft);
    await expect(firstRow.getByLabel("Edit message")).toBeFocused();
    await expect(secondRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
    await expect(threadPane.getByRole("button", { name: "Close thread" })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`/${routedMessage.route_id}$`));
  } finally {
    threadRelease.resolve();
    await threadDelivered.promise;
  }
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page).toHaveURL(new RegExp(`/${channel.route_id}$`));
  await expect(threadPane.getByRole("button", { name: "Close thread" })).toHaveCount(0);
  await firstRow.getByLabel("Edit message").press("Escape");
  await expect(firstRow.getByRole("button", { name: "More actions" })).toBeFocused();

  let patchCount = 0;
  page.on("request", (request) => {
    if (request.method() === "PATCH" && request.url().includes("/api/messages/")) {
      patchCount += 1;
    }
  });
  await openTimelineEditor(secondRow);
  await secondRow.getByLabel("Edit message").fill("\u0085");
  await secondRow.getByLabel("Edit message").press("Control+Enter");
  await expect(secondRow.getByLabel("Edit message")).toHaveValue("\u0085");
  await expect(secondRow.getByRole("alert")).toHaveText("Message body is required");
  expect(patchCount).toBe(0);

  await secondRow.getByLabel("Edit message").fill(`\u0085${secondBody}\u0085`);
  await secondRow.getByRole("button", { name: "Save" }).click();
  await expect(secondRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  expect(patchCount).toBe(0);

  await openTimelineEditor(secondRow);
  await secondRow.getByLabel("Edit message").fill("\ufeff");
  await secondRow.getByRole("button", { name: "Save" }).click();
  await expect(secondRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  const preservedFEFFResponse = await page.request.get(`/api/messages/${secondMessage.id}`);
  expect(preservedFEFFResponse.ok()).toBe(true);
  const preservedFEFF = (await preservedFEFFResponse.json()) as { message: { body: string } };
  expect(preservedFEFF.message.body).toBe("\ufeff");

  let releaseFirstSave!: () => void;
  let markFirstSaveStarted!: () => void;
  const firstSaveGate = new Promise<void>((resolve) => {
    releaseFirstSave = resolve;
  });
  const firstSaveStarted = new Promise<void>((resolve) => {
    markFirstSaveStarted = resolve;
  });
  await page.route(`**/api/messages/${firstMessage.id}`, async (route) => {
    if (route.request().method() !== "PATCH") {
      await route.continue();
      return;
    }
    markFirstSaveStarted();
    await firstSaveGate;
    await route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ error: "deliberate edit failure" }),
    });
  });

  await openTimelineEditor(firstRow);
  await firstRow.getByLabel("Edit message").fill(`Saved first edit ${suffix}`);
  await firstRow.getByRole("button", { name: "Save" }).click();
  await firstSaveStarted;

  await openTimelineEditor(secondRow);
  await expect(firstRow.getByLabel("Edit message")).toHaveValue(`Saved first edit ${suffix}`);
  await expect(secondRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  releaseFirstSave();
  await expect(firstRow.getByRole("alert")).toHaveText("deliberate edit failure");
  await expect(firstRow.getByLabel("Edit message")).toHaveValue(`Saved first edit ${suffix}`);
});

test("virtualized edit rows retain and reveal their unsaved draft", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Editing Virtualized ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const otherUserID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspace.id,
    "--name",
    `Editing Alternate ${suffix}`,
    "--email",
    `editing-alternate-${suffix}@example.com`,
  ]);
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `editing-virtualized-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };

  const created: Array<{ id: string; body: string }> = [];
  for (let index = 0; index < 65; index += 1) {
    const body = `virtualized-edit-${String(index).padStart(3, "0")}-${suffix}`;
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: index % 2 === 0 ? undefined : { "X-ClickClack-User": otherUserID },
      data: { body },
    });
    expect(response.ok()).toBe(true);
    const data = (await response.json()) as { message: { id: string } };
    created.push({ id: data.message.id, body });
  }

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const scrollport = page.locator(".messages-scroll");
  await scrollport.evaluate((element) => {
    element.scrollTop = 0;
  });

  const firstRow = page.locator(`[data-message-id="${created[0].id}"]`);
  await expect(firstRow).toBeVisible();
  await openTimelineEditor(firstRow);
  const draft = `retained virtualized draft ${suffix}`;
  await firstRow.getByLabel("Edit message").fill(draft);
  await firstRow.getByLabel("Edit message").blur();

  await scrollport.evaluate(async (element) => {
    for (let frame = 0; frame < 12; frame += 1) {
      element.scrollTop = element.scrollHeight;
      element.dispatchEvent(new Event("scroll", { bubbles: true }));
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    }
  });
  await expect(firstRow).toHaveCount(0);

  const competingMessageID = created[created.length - 1].id;
  const competingRow = page.locator(`[data-message-id="${competingMessageID}"]`);
  await expect(competingRow).toBeVisible();
  await openTimelineEditor(competingRow);

  await expect(firstRow.getByLabel("Edit message")).toHaveValue(draft);
  await expect(firstRow.getByLabel("Edit message")).toBeFocused();
  await expect(page.locator('.messages textarea[aria-label="Edit message"]')).toHaveCount(1);
});

test("message editing works in direct conversations", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Editing Direct ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const otherUserID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspace.id,
    "--name",
    `Editing Direct User ${suffix}`,
    "--email",
    `editing-direct-${suffix}@example.com`,
  ]);
  const directResponse = await page.request.post("/api/dms", {
    data: { workspace_id: workspace.id, member_ids: [otherUserID] },
  });
  expect(directResponse.ok()).toBe(true);
  const { conversation } = (await directResponse.json()) as {
    conversation: { id: string; route_id: string };
  };
  const originalBody = `Original direct message ${suffix}`;
  const messageResponse = await page.request.post(`/api/dms/${conversation.id}/messages`, {
    data: { body: originalBody },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = (await messageResponse.json()) as {
    message: { id: string };
  };

  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await waitForAppReady(page);
  const row = page.locator(`[data-message-id="${message.id}"]`);
  await expect(row).toContainText(originalBody);
  await openTimelineEditor(row);
  const editedBody = `Edited direct message ${suffix}`;
  await row.getByLabel("Edit message").fill(editedBody);
  await row.getByRole("button", { name: "Save" }).click();
  await expect(row.locator(".markdown")).toContainText(editedBody);
  await expect(row.getByText("(edited)")).toBeVisible();
});
