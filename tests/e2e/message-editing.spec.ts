import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

function clickclack(args: string[]): string {
  return execFileSync("go", ["run", "./apps/api/cmd/clickclack", ...args], {
    cwd: process.cwd(),
    encoding: "utf8",
  }).trim();
}

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
  await channelRow.hover();
  await channelRow.getByRole("button", { name: "Edit message" }).click();
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

  await channelRow.getByRole("button", { name: "Open thread" }).click();
  const threadPane = page.getByLabel("Thread pane", { exact: true });
  await expect(threadPane).toBeVisible();
  await channelRow.getByRole("button", { name: "Edit message" }).focus();
  await expect(channelRow.locator(".message-actions")).toHaveCSS("opacity", "1");
  await channelRow.getByRole("button", { name: "Edit message" }).click();
  await expect(page.locator('textarea[aria-label="Edit message"]')).toHaveCount(1);
  await expect(channelRow.getByLabel("Edit message")).toBeFocused();
  await channelRow.getByLabel("Edit message").press("Escape");
  await expect(channelRow.getByRole("button", { name: "Edit message" })).toBeFocused();
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
  await channelRow.hover();
  await channelRow.getByRole("button", { name: "Edit message" }).click();
  await expect(channelRow.getByLabel("Edit message")).toBeFocused();
  await channelRow.getByLabel("Edit message").press("Escape");
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
  await row.hover();
  await row.getByRole("button", { name: "Edit message" }).click();
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
    channel: { name: string };
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
  const firstRow = page.locator(`[data-message-id="${firstMessage.id}"]`);
  const secondRow = page.locator(`[data-message-id="${secondMessage.id}"]`);

  await firstRow.hover();
  await firstRow.getByRole("button", { name: "Edit message" }).click();
  const unsavedDraft = `Unsaved first edit ${suffix}`;
  await firstRow.getByLabel("Edit message").fill(unsavedDraft);
  await page.getByRole("link", { name: `# ${alternateChannel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${alternateChannel.name}` })).toBeVisible();
  await expect(page.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await expect(firstRow.getByLabel("Edit message")).toHaveValue(unsavedDraft);
  await secondRow.hover();
  await secondRow.getByRole("button", { name: "Edit message" }).click();
  await expect(firstRow.getByLabel("Edit message")).toHaveValue(unsavedDraft);
  await expect(secondRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  await firstRow.getByLabel("Edit message").press("Escape");
  await expect(firstRow.getByRole("button", { name: "Edit message" })).toBeFocused();

  let patchCount = 0;
  page.on("request", (request) => {
    if (request.method() === "PATCH" && request.url().includes("/api/messages/")) {
      patchCount += 1;
    }
  });
  await secondRow.hover();
  await secondRow.getByRole("button", { name: "Edit message" }).click();
  await secondRow.getByLabel("Edit message").fill("\u0085");
  await secondRow.getByLabel("Edit message").press("Control+Enter");
  await expect(secondRow.getByLabel("Edit message")).toHaveValue("\u0085");
  await expect(secondRow.getByRole("alert")).toHaveText("Message body is required");
  expect(patchCount).toBe(0);

  await secondRow.getByLabel("Edit message").fill(`\u0085${secondBody}\u0085`);
  await secondRow.getByRole("button", { name: "Save" }).click();
  await expect(secondRow.locator('textarea[aria-label="Edit message"]')).toHaveCount(0);
  expect(patchCount).toBe(0);

  await secondRow.hover();
  await secondRow.getByRole("button", { name: "Edit message" }).click();
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

  await firstRow.hover();
  await firstRow.getByRole("button", { name: "Edit message" }).click();
  await firstRow.getByLabel("Edit message").fill(`Saved first edit ${suffix}`);
  await firstRow.getByRole("button", { name: "Save" }).click();
  await firstSaveStarted;

  await secondRow.hover();
  await secondRow.getByRole("button", { name: "Edit message" }).click();
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
  await firstRow.hover();
  await firstRow.getByRole("button", { name: "Edit message" }).click();
  const draft = `retained virtualized draft ${suffix}`;
  await firstRow.getByLabel("Edit message").fill(draft);

  await scrollport.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await expect(firstRow).toHaveCount(0);

  const visibleEditableRows = page
    .locator(".message-row:visible")
    .filter({ has: page.getByRole("button", { name: "Edit message" }) });
  await expect
    .poll(async () => visibleEditableRows.count(), {
      message: "at least one distinct editable row should be rendered",
    })
    .toBeGreaterThan(1);
  const visibleEditableMessageIDs = await visibleEditableRows.evaluateAll((rows) =>
    rows
      .map((row) => row.getAttribute("data-message-id"))
      .filter((id): id is string => Boolean(id)),
  );
  const competingMessageID = visibleEditableMessageIDs.find((id) => id !== created[0].id);
  expect(competingMessageID).toBeTruthy();
  const competingRow = page.locator(`[data-message-id="${competingMessageID}"]`);
  await competingRow.hover();
  await competingRow.getByRole("button", { name: "Edit message" }).click();

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
  await row.hover();
  await row.getByRole("button", { name: "Edit message" }).click();
  const editedBody = `Edited direct message ${suffix}`;
  await row.getByLabel("Edit message").fill(editedBody);
  await row.getByRole("button", { name: "Save" }).click();
  await expect(row.locator(".markdown")).toContainText(editedBody);
  await expect(row.getByText("(edited)")).toBeVisible();
});
