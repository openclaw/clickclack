import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

async function openReactionChannel(page: Page) {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Reaction Proof ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `reaction-proof-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  return { suffix, workspace, channel };
}

test("message reactions update immediately, persist, and reconcile through realtime", async ({
  page,
}) => {
  const { suffix } = await openReactionChannel(page);

  const body = `Reaction behavior proof ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();

  await row.getByRole("button", { name: "Add reaction" }).click();
  const addReactionResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/messages/${messageID}/reactions`),
  );
  await row.getByRole("menuitem", { name: "React with 👍" }).click();
  const reaction = row.getByRole("button", { name: "👍 — 1 reaction" });
  await expect(reaction).toBeVisible();
  if (process.env.REACTION_PROOF_PATH) {
    await row.scrollIntoViewIfNeeded();
    await page.screenshot({ path: process.env.REACTION_PROOF_PATH, fullPage: true });
  }
  expect((await addReactionResponse).ok()).toBe(true);

  await page.reload();
  await waitForAppReady(page);
  const persistedRow = page.locator(`[data-message-id="${messageID}"]`);
  await expect(persistedRow.getByRole("button", { name: "👍 — 1 reaction" })).toBeVisible();

  const removeResponse = await page.request.delete(
    `/api/messages/${messageID}/reactions/${encodeURIComponent("👍")}`,
  );
  expect(removeResponse.ok()).toBe(true);
  await expect(persistedRow.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);

  let releaseHeldAdd!: () => void;
  let markHeldAddCommitted!: () => void;
  const heldAddGate = new Promise<void>((resolve) => {
    releaseHeldAdd = resolve;
  });
  const heldAddCommitted = new Promise<void>((resolve) => {
    markHeldAddCommitted = resolve;
  });
  await page.route("**/api/messages/*/reactions", async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    markHeldAddCommitted();
    await heldAddGate;
    await route.fulfill({ response });
  });
  await persistedRow.getByRole("button", { name: "Add reaction" }).click();
  await persistedRow.getByRole("menuitem", { name: "React with 👍" }).click();
  await heldAddCommitted;
  await expect(persistedRow.getByRole("button", { name: "Add reaction" })).toBeDisabled();
  const newerRemoval = await page.request.delete(
    `/api/messages/${messageID}/reactions/${encodeURIComponent("👍")}`,
  );
  expect(newerRemoval.ok()).toBe(true);
  releaseHeldAdd();
  await expect(persistedRow.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);
});

test("a conflicting add reconciles the already-committed reaction", async ({ page }) => {
  const { suffix } = await openReactionChannel(page);
  const body = `Conflicting reaction add ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user } = (await meResponse.json()) as { user: { id: string } };

  await page.route(`**/api/messages/${messageID}/reactions`, async (route) => {
    await route.fulfill({
      status: 409,
      contentType: "application/json",
      body: '{"error":"reaction already exists"}',
    });
  });
  await page.route(`**/api/messages/${messageID}`, async (route) => {
    const response = await route.fetch();
    const data = (await response.json()) as { message: { reactions?: unknown[] } };
    data.message.reactions = [
      { emoji: "👍", user_id: user.id, created_at: new Date().toISOString() },
    ];
    await route.fulfill({ response, json: data });
  });

  await row.getByRole("button", { name: "Add reaction" }).click();
  await row.getByRole("menuitem", { name: "React with 👍" }).click();
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toBeVisible();
  await expect(row.getByRole("status")).toContainText("reaction already exists");
});

test("a committed reaction survives a failed refresh after an unrelated update", async ({
  page,
}) => {
  const { suffix } = await openReactionChannel(page);
  const body = `Committed reaction refresh ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();

  let releaseMutation!: () => void;
  const mutationGate = new Promise<void>((resolve) => {
    releaseMutation = resolve;
  });
  await page.route(`**/api/messages/${messageID}/reactions`, async (route) => {
    const requestBody = route.request().postDataJSON() as { emoji?: string } | null;
    if (route.request().method() !== "POST" || requestBody?.emoji !== "👍") {
      await route.continue();
      return;
    }
    await mutationGate;
    const response = await route.fetch();
    await route.fulfill({ response });
  });
  let refreshCount = 0;
  await page.route(`**/api/messages/${messageID}`, async (route) => {
    refreshCount += 1;
    if (refreshCount === 1) {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 500,
      contentType: "application/json",
      body: '{"error":"refresh failed"}',
    });
  });

  await row.getByRole("button", { name: "Add reaction" }).click();
  const committedResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/messages/${messageID}/reactions`) &&
      (response.request().postDataJSON() as { emoji?: string } | null)?.emoji === "👍",
  );
  await row.getByRole("menuitem", { name: "React with 👍" }).click();
  const unrelatedResponse = await page.request.post(`/api/messages/${messageID}/reactions`, {
    data: { emoji: "❤️" },
  });
  expect(unrelatedResponse.ok()).toBe(true);
  await expect(row.getByRole("button", { name: "❤️ — 1 reaction" })).toBeVisible();
  releaseMutation();
  expect((await committedResponse).ok()).toBe(true);
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toBeVisible();
  await expect(row.getByRole("status")).toHaveCount(0);
});

test("a failed optimistic reaction rolls back when recovery also fails after realtime", async ({
  page,
}) => {
  const { suffix } = await openReactionChannel(page);
  const body = `Concurrent reaction rollback ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();

  let releaseFailedRequest!: () => void;
  const failedRequestGate = new Promise<void>((resolve) => {
    releaseFailedRequest = resolve;
  });
  await page.route("**/api/messages/*/reactions", async (route) => {
    const body = route.request().postDataJSON() as { emoji?: string } | null;
    if (route.request().method() === "POST" && body?.emoji === "👍") {
      await failedRequestGate;
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: '{"error":"failed"}',
      });
      return;
    }
    await route.continue();
  });
  let messageRefreshCount = 0;
  await page.route(`**/api/messages/${messageID}`, async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    messageRefreshCount += 1;
    if (messageRefreshCount > 1) {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: '{"error":"recovery failed"}',
      });
      return;
    }
    await route.continue();
  });

  await row.getByRole("button", { name: "Add reaction" }).click();
  await row.getByRole("menuitem", { name: "React with 👍" }).click();
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toBeVisible();

  const successfulResponse = await page.request.post(`/api/messages/${messageID}/reactions`, {
    data: { emoji: "❤️" },
  });
  expect(successfulResponse.ok()).toBe(true);
  const successfulReaction = row.getByRole("button", { name: "❤️ — 1 reaction" });
  await expect(successfulReaction).toBeVisible();
  await expect.poll(() => messageRefreshCount).toBe(1);
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toBeVisible();

  releaseFailedRequest();
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);
  await expect(successfulReaction).toBeVisible();
  await expect(row.getByRole("status")).toContainText("failed");
});

test("an ambiguous failed removal reconciles with the committed server state", async ({ page }) => {
  const { suffix } = await openReactionChannel(page);
  const body = `Ambiguous reaction removal ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();

  await row.getByRole("button", { name: "Add reaction" }).click();
  const addResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/messages/${messageID}/reactions`),
  );
  await row.getByRole("menuitem", { name: "React with 👍" }).click();
  expect((await addResponse).ok()).toBe(true);
  const reaction = row.getByRole("button", { name: "👍 — 1 reaction" });
  await expect(reaction).toBeVisible();

  await page.route(
    `**/api/messages/${messageID}/reactions/${encodeURIComponent("👍")}`,
    async (route) => {
      await route.fetch();
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: '{"error":"response lost"}',
      });
    },
  );
  await reaction.click();
  await expect(reaction).toHaveCount(0);
  await expect(row.getByRole("status")).toContainText("response lost");
  await page.waitForTimeout(100);
  await expect(reaction).toHaveCount(0);
});

test("realtime reactions preserve an around-search history window", async ({ page }) => {
  const { channel } = await openReactionChannel(page);
  const created: Array<{ id: string }> = [];
  for (let index = 0; index < 140; index++) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: `reaction-history-${String(index).padStart(3, "0")}` },
    });
    expect(response.ok()).toBe(true);
    const data = (await response.json()) as { message: { id: string } };
    created.push(data.message);
  }
  await page.reload();
  await waitForAppReady(page);

  await page.getByLabel("Search messages").fill("reaction-history-010");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  const result = page
    .getByLabel("Search results")
    .locator(".search-result", { hasText: "reaction-history-010" });
  await expect(result).toBeVisible();
  await result.click();
  const target = page.locator(`[data-message-id="${created[10].id}"]`);
  const neighbor = page.locator(`[data-message-id="${created[11].id}"]`);
  await expect(target).toBeVisible();
  await expect(neighbor).toBeVisible();

  const reactionResponse = await page.request.post(`/api/messages/${created[10].id}/reactions`, {
    data: { emoji: "👀" },
  });
  expect(reactionResponse.ok()).toBe(true);
  await expect(target.getByRole("button", { name: "👀 — 1 reaction" })).toBeVisible();
  await expect(neighbor).toBeVisible();
});

test("a stale realtime reaction refresh cannot overwrite a newer event", async ({ page }) => {
  const { suffix } = await openReactionChannel(page);
  const body = `Reaction refresh race ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();

  let releaseFirstRefresh!: () => void;
  let markFirstRefreshStarted!: () => void;
  let releaseThirdRefresh!: () => void;
  let markThirdRefreshStarted!: () => void;
  const firstRefreshGate = new Promise<void>((resolve) => {
    releaseFirstRefresh = resolve;
  });
  const firstRefreshStarted = new Promise<void>((resolve) => {
    markFirstRefreshStarted = resolve;
  });
  const thirdRefreshGate = new Promise<void>((resolve) => {
    releaseThirdRefresh = resolve;
  });
  const thirdRefreshStarted = new Promise<void>((resolve) => {
    markThirdRefreshStarted = resolve;
  });
  let refreshCount = 0;
  await page.route(`**/api/messages/${messageID}`, async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    refreshCount += 1;
    const response = await route.fetch();
    if (refreshCount === 1) {
      markFirstRefreshStarted();
      await firstRefreshGate;
    } else if (refreshCount === 3) {
      markThirdRefreshStarted();
      await thirdRefreshGate;
    }
    await route.fulfill({ response });
  });

  const addResponse = await page.request.post(`/api/messages/${messageID}/reactions`, {
    data: { emoji: "👍" },
  });
  expect(addResponse.ok()).toBe(true);
  await firstRefreshStarted;

  const removeResponse = await page.request.delete(
    `/api/messages/${messageID}/reactions/${encodeURIComponent("👍")}`,
  );
  expect(removeResponse.ok()).toBe(true);
  await expect.poll(() => refreshCount).toBe(2);
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);

  const newerAddResponse = await page.request.post(`/api/messages/${messageID}/reactions`, {
    data: { emoji: "👀" },
  });
  expect(newerAddResponse.ok()).toBe(true);
  await thirdRefreshStarted;
  releaseFirstRefresh();
  await expect.poll(() => refreshCount).toBe(3);
  await page.waitForTimeout(100);
  await expect(row.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);
  releaseThirdRefresh();
  await expect(row.getByRole("button", { name: "👀 — 1 reaction" })).toBeVisible();
});

test("a reaction refresh cannot overwrite a concurrent message edit", async ({ page }) => {
  const { suffix } = await openReactionChannel(page);
  const originalBody = `Reaction edit race ${suffix}`;
  const editedBody = `Edited during reaction refresh ${suffix}`;
  await page.getByLabel("Message body").fill(originalBody);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: originalBody });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();
  const stableRow = page.locator(`[data-message-id="${messageID}"]`);

  let releaseRefresh!: () => void;
  let markRefreshStarted!: () => void;
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  const refreshStarted = new Promise<void>((resolve) => {
    markRefreshStarted = resolve;
  });
  await page.route(`**/api/messages/${messageID}`, async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    markRefreshStarted();
    await refreshGate;
    await route.fulfill({ response });
  });

  const addResponse = await page.request.post(`/api/messages/${messageID}/reactions`, {
    data: { emoji: "👍" },
  });
  expect(addResponse.ok()).toBe(true);
  await refreshStarted;
  const editResponse = await page.request.patch(`/api/messages/${messageID}`, {
    data: { body: editedBody },
  });
  expect(editResponse.ok()).toBe(true);
  await expect(stableRow.locator(".markdown")).toContainText(editedBody);

  releaseRefresh();
  await page.waitForTimeout(100);
  await expect(stableRow.locator(".markdown")).toContainText(editedBody);
});

test("a reaction refresh started in an old view cannot overwrite fresh navigation state", async ({
  page,
}) => {
  const { suffix, workspace, channel } = await openReactionChannel(page);
  const otherChannelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `reaction-other-${suffix}`, kind: "public" },
  });
  expect(otherChannelResponse.ok()).toBe(true);
  const { channel: otherChannel } = (await otherChannelResponse.json()) as {
    channel: { route_id: string };
  };
  const body = `Reaction navigation race ${suffix}`;
  await page.getByLabel("Message body").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)", { hasText: body });
  await expect(row).toBeVisible();
  const messageID = await row.getAttribute("data-message-id");
  expect(messageID).toBeTruthy();

  let releaseRefresh!: () => void;
  let markRefreshStarted!: () => void;
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  const refreshStarted = new Promise<void>((resolve) => {
    markRefreshStarted = resolve;
  });
  await page.route(`**/api/messages/${messageID}`, async (route) => {
    const response = await route.fetch();
    markRefreshStarted();
    await refreshGate;
    await route.fulfill({ response });
  });

  const addResponse = await page.request.post(`/api/messages/${messageID}/reactions`, {
    data: { emoji: "👍" },
  });
  expect(addResponse.ok()).toBe(true);
  await refreshStarted;
  await page.goto(`/app/${workspace.route_id}/${otherChannel.route_id}`);
  await waitForAppReady(page);
  const removeResponse = await page.request.delete(
    `/api/messages/${messageID}/reactions/${encodeURIComponent("👍")}`,
  );
  expect(removeResponse.ok()).toBe(true);
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const freshRow = page.locator(`[data-message-id="${messageID}"]`);
  await expect(freshRow.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);

  releaseRefresh();
  await page.waitForTimeout(100);
  await expect(freshRow.getByRole("button", { name: "👍 — 1 reaction" })).toHaveCount(0);
});

test("a reaction event received during message loading survives the stale page response", async ({
  page,
}) => {
  const { suffix, channel } = await openReactionChannel(page);
  const body = `Reaction load race ${suffix}`;
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = (await messageResponse.json()) as { message: { id: string } };

  let releaseMessagePage!: () => void;
  let markMessagePageStarted!: () => void;
  const messagePageGate = new Promise<void>((resolve) => {
    releaseMessagePage = resolve;
  });
  const messagePageStarted = new Promise<void>((resolve) => {
    markMessagePageStarted = resolve;
  });
  let heldPage = false;
  await page.route(`**/api/channels/${channel.id}/messages?*`, async (route) => {
    if (heldPage) {
      await route.continue();
      return;
    }
    heldPage = true;
    const response = await route.fetch();
    markMessagePageStarted();
    await messagePageGate;
    await route.fulfill({ response });
  });

  await page.reload({ waitUntil: "domcontentloaded" });
  await messagePageStarted;
  const reactionResponse = await page.request.post(`/api/messages/${message.id}/reactions`, {
    data: { emoji: "👀" },
  });
  expect(reactionResponse.ok()).toBe(true);
  await page.waitForTimeout(100);
  releaseMessagePage();
  await waitForAppReady(page);

  const row = page.locator(`[data-message-id="${message.id}"]`);
  await expect(row.getByRole("button", { name: "👀 — 1 reaction" })).toBeVisible();
});
