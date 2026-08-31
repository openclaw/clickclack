import { expect, test } from "@playwright/test";
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

test("topic selector, labels, filter, clear, and realtime stay coherent", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 10);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Topic UX ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "topic-lab", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const secondChannelResponse = await page.request.post(
    `/api/workspaces/${workspace.id}/channels`,
    {
      data: { name: "topic-neighbor", kind: "public" },
    },
  );
  expect(secondChannelResponse.ok()).toBe(true);
  const { channel: secondChannel } = (await secondChannelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  const topicResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Release", channel_id: channel.id },
  });
  expect(topicResponse.ok()).toBe(true);
  const { topic } = (await topicResponse.json()) as {
    topic: { id: string };
  };
  const otherTopicResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Design" },
  });
  expect(otherTopicResponse.ok()).toBe(true);
  const { topic: otherTopic } = (await otherTopicResponse.json()) as {
    topic: { id: string };
  };

  async function currentChannelState(): Promise<{ last_read_seq?: number; unread_count?: number }> {
    const response = await page.request.get(`/api/workspaces/${workspace.id}/channels`);
    const data = (await response.json()) as {
      channels: { id: string; last_read_seq?: number; unread_count?: number }[];
    };
    const current = data.channels.find((candidate) => candidate.id === channel.id);
    if (!current) throw new Error("channel missing from list");
    return current;
  }

  await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "untagged baseline" },
  });
  await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "release baseline", topic_id: topic.id },
  });
  await page.request.post(`/api/channels/${secondChannel.id}/messages`, {
    data: { body: "neighbor untagged" },
  });
  const senderID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspace.id,
    "--name",
    `Topic Sender ${suffix}`,
    "--email",
    `topic-sender-${suffix}@example.com`,
  ]);

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);

  const topicSelect = page.getByLabel("Message topic");
  await expect(topicSelect).toBeVisible();
  const refreshedTopicResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Operations" },
  });
  expect(refreshedTopicResponse.ok()).toBe(true);
  await page.getByLabel("Message body").focus();
  await expect(topicSelect.locator("option", { hasText: "Operations" })).toHaveCount(1);
  await topicSelect.selectOption({ label: "Release" });
  await page.getByLabel("Message body").fill("release from composer");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByText("release from composer")).toBeVisible();
  await expect(page.getByRole("button", { name: "Filter by topic Release" }).last()).toBeVisible();

  let releasePendingSend: (() => void) | undefined;
  const pendingSendBlocked = new Promise<void>((resolve) => {
    releasePendingSend = resolve;
  });
  let pendingSendSeen: (() => void) | undefined;
  const pendingSendRequested = new Promise<void>((resolve) => {
    pendingSendSeen = resolve;
  });
  await page.route(`**/api/channels/${channel.id}/messages`, async (route) => {
    const request = route.request();
    const body = request.method() === "POST" ? request.postDataJSON() : undefined;
    if (body?.body === "untagged pending while filtering") {
      pendingSendSeen?.();
      await pendingSendBlocked;
    }
    await route.continue();
  });
  await topicSelect.selectOption("");
  await page.getByLabel("Message body").fill("untagged pending while filtering");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await pendingSendRequested;
  await expect(page.getByText("untagged pending while filtering")).toBeVisible();

  await page.getByRole("button", { name: "Filter by topic Release" }).first().click();
  await expect(page.getByText("Showing topic")).toContainText("Release");
  await expect(page.getByText("untagged baseline")).toHaveCount(0);
  await expect(page.getByText("untagged pending while filtering")).toHaveCount(0);
  await expect(page.getByText("release baseline")).toBeVisible();
  releasePendingSend?.();
  const filteredReadSeq = (await currentChannelState()).last_read_seq || 0;
  await expect(page.locator(".messages")).not.toHaveClass(/is-revealing/);
  await expect
    .poll(() =>
      page.locator(".messages-scroll").evaluate((element) => getComputedStyle(element).opacity),
    )
    .toBe("1");
  if (process.env.TOPIC_FILTER_PROOF_PATH) {
    await page.screenshot({ path: process.env.TOPIC_FILTER_PROOF_PATH, fullPage: true });
  }

  const nonmatchingResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    headers: { "X-ClickClack-User": senderID },
    data: { body: "design realtime", topic_id: otherTopic.id },
  });
  expect(nonmatchingResponse.ok()).toBe(true);
  await page.waitForTimeout(250);
  await expect(page.getByText("design realtime")).toHaveCount(0);

  const matchingResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    headers: { "X-ClickClack-User": senderID },
    data: { body: "release realtime", topic_id: topic.id },
  });
  expect(matchingResponse.ok()).toBe(true);
  await expect(page.getByText("release realtime")).toBeVisible();
  await page.getByLabel("Message body").fill("release while filtered");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByText("release while filtered")).toBeVisible();
  await page.waitForTimeout(400);
  expect((await currentChannelState()).last_read_seq || 0).toBe(filteredReadSeq);
  expect((await currentChannelState()).unread_count || 0).toBe(2);

  await page.getByRole("link", { name: `# ${secondChannel.name}`, exact: true }).click();
  await expect(page.getByRole("heading", { name: `#${secondChannel.name}` })).toBeVisible();
  await expect(page.getByText("Showing topic")).toHaveCount(0);
  await expect(page.getByText("neighbor untagged")).toBeVisible();
  const originalChannelLink = page
    .locator("#sidebar-channels-list")
    .locator("a.nav-item.channel", { hasText: "topic-lab" });
  await expect(originalChannelLink.getByLabel("2 unread", { exact: true })).toBeVisible();

  await originalChannelLink.click();
  await expect(page.getByRole("heading", { name: "#topic-lab" })).toBeVisible();
  const captured = deferred(),
    release = deferred(),
    delivered = deferred();
  let held = false;
  await page.route(`**/api/channels/${channel.id}/messages?*`, async (route) => {
    const query = new URL(route.request().url()).searchParams;
    if (held || query.get("topic_id") !== topic.id) return route.continue();
    held = true;
    expect(query.has("around_seq")).toBe(false);
    const response = await route.fetch();
    captured.resolve();
    await release.promise;
    await route.fulfill({ response });
    delivered.resolve();
  });
  try {
    await page.getByRole("button", { name: "Filter by topic Release" }).first().click();
    await captured.promise;
    await expect(page.getByText("Showing topic")).toContainText("Release");
    await page.getByRole("button", { name: "Clear filter" }).click();
    await expect(page.getByText("Showing topic")).toHaveCount(0);
    await expect(page.getByText("untagged baseline")).toBeVisible();
    release.resolve();
    await delivered.promise;
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    await expect(page.getByText("Showing topic")).toHaveCount(0);
    await expect(page.getByText("untagged baseline")).toBeVisible();
    await expect(page.getByText("design realtime")).toBeVisible();
  } finally {
    release.resolve();
  }

  await page.setViewportSize({ width: 390, height: 760 });
  const closeNavigation = page.getByLabel("Close navigation");
  if (await closeNavigation.isVisible()) await closeNavigation.click();
  await page.waitForTimeout(250);
  await expect(topicSelect).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      ),
    )
    .toBe(true);
  if (process.env.TOPIC_MOBILE_PROOF_PATH) {
    await page.screenshot({ path: process.env.TOPIC_MOBILE_PROOF_PATH, fullPage: true });
  }
});

test("topic refresh and off-filter send failures recover visibly", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 10);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Topic recovery ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "topic-recovery", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const topicResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Release", channel_id: channel.id },
  });
  expect(topicResponse.ok()).toBe(true);
  const { topic } = (await topicResponse.json()) as { topic: { id: string } };
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "release recovery baseline", topic_id: topic.id },
  });
  expect(messageResponse.ok()).toBe(true);

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const topicSelect = page.getByLabel("Message topic");
  await page.getByRole("button", { name: "Filter by topic Release" }).click();
  await expect(page.getByText("Showing topic")).toContainText("Release");

  await topicSelect.selectOption("");
  let failNextUnfilteredReload = true;
  await page.route(`**/api/channels/${channel.id}/messages*`, async (route) => {
    const request = route.request();
    const body = request.method() === "POST" ? request.postDataJSON() : undefined;
    if (body?.body === "off-filter failure") {
      await route.abort("failed");
      return;
    }
    const url = new URL(request.url());
    if (
      request.method() === "GET" &&
      failNextUnfilteredReload &&
      !url.searchParams.has("topic_id")
    ) {
      failNextUnfilteredReload = false;
      await route.abort("failed");
      return;
    }
    await route.continue();
  });
  await page.getByLabel("Message body").fill("off-filter failure");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByText("Showing topic")).toHaveCount(0);
  await expect(page.getByText("off-filter failure")).toBeVisible();
  await expect(page.getByText("release recovery baseline")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
  const refreshResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "same-view realtime refresh" },
  });
  expect(refreshResponse.ok()).toBe(true);
  await expect(page.getByText("same-view realtime refresh")).toBeVisible();
  await expect(page.getByText("off-filter failure")).toHaveCount(1);
  await page.getByRole("button", { name: "Discard" }).click();

  await page.getByRole("button", { name: "Filter by topic Release" }).click();
  await expect(page.getByText("Showing topic")).toContainText("Release");
  const topicsPath = `**/api/workspaces/${workspace.id}/topics`;
  await page.route(topicsPath, async (route) => route.abort("failed"));
  const failedTopicsRefresh = page.waitForRequest(
    (request) =>
      request.method() === "GET" &&
      request.url().includes(`/api/workspaces/${workspace.id}/topics`),
  );
  await page.getByLabel("Message body").focus();
  await failedTopicsRefresh;
  await expect(page.getByText("Showing topic")).toContainText("Release");
  await page.unroute(topicsPath);
  await page.getByLabel("Message body").press("Tab");
  const unfilteredReload = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      request.method() === "GET" &&
      url.pathname === `/api/channels/${channel.id}/messages` &&
      !url.searchParams.has("topic_id")
    );
  });
  await page.route(topicsPath, async (route) => {
    await route.fulfill({ json: { topics: [] } });
  });
  failNextUnfilteredReload = true;
  await page.getByLabel("Message body").focus();
  await unfilteredReload;
  await expect(page.getByText("Showing topic")).toHaveCount(0);
  await expect(topicSelect).toHaveCount(0);
  await expect(page.getByText("release recovery baseline")).toHaveCount(0);
  await expect(page.getByText(/Topic changed, but messages could not reload/)).toBeVisible();
});

test("failed topic drafts stay with their conversation during navigation", async ({ page }) => {
  test.slow();
  const suffix = randomUUID().replaceAll("-", "").slice(0, 10);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Topic navigation ${suffix}` },
  });
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const firstChannelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "topic-origin", kind: "public" },
  });
  const secondChannelResponse = await page.request.post(
    `/api/workspaces/${workspace.id}/channels`,
    { data: { name: "topic-destination", kind: "public" } },
  );
  expect(workspaceResponse.ok()).toBe(true);
  expect(firstChannelResponse.ok()).toBe(true);
  expect(secondChannelResponse.ok()).toBe(true);
  const { channel: firstChannel } = (await firstChannelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const topicResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Release", channel_id: firstChannel.id },
  });
  expect(topicResponse.ok()).toBe(true);
  const { topic } = (await topicResponse.json()) as { topic: { id: string } };
  await page.request.post(`/api/channels/${firstChannel.id}/messages`, {
    data: { body: "origin topic message", topic_id: topic.id },
  });

  await page.goto(`/app/${workspace.route_id}/${firstChannel.route_id}`);
  await waitForAppReady(page);
  await page.getByRole("button", { name: "Filter by topic Release" }).click();
  await page.getByLabel("Message topic").selectOption("");

  let releaseReload: (() => void) | undefined;
  const reloadBlocked = new Promise<void>((resolve) => {
    releaseReload = resolve;
  });
  let reloadSeen: (() => void) | undefined;
  const reloadRequested = new Promise<void>((resolve) => {
    reloadSeen = resolve;
  });
  let sendAttempts = 0;
  let releaseRetry: (() => void) | undefined;
  const retryBlocked = new Promise<void>((resolve) => {
    releaseRetry = resolve;
  });
  await page.route(`**/api/channels/${firstChannel.id}/messages*`, async (route) => {
    const request = route.request();
    const body = request.method() === "POST" ? request.postDataJSON() : undefined;
    if (body?.body === "failed in origin") {
      sendAttempts += 1;
      if (sendAttempts > 1) {
        await retryBlocked;
      }
      await route.abort("failed");
      return;
    }
    const url = new URL(request.url());
    if (request.method() === "GET" && !url.searchParams.has("topic_id")) {
      reloadSeen?.();
      await reloadBlocked;
    }
    await route.continue();
  });
  await page.getByLabel("Message body").fill("failed in origin");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await reloadRequested;
  await page.getByRole("link", { name: "# topic-destination", exact: true }).click();
  await expect(page.getByRole("heading", { name: "#topic-destination" })).toBeVisible();
  releaseReload?.();
  await expect(page.getByText("failed in origin")).toHaveCount(0);

  await page.getByRole("link", { name: "# topic-origin", exact: true }).click();
  await expect(page.getByRole("heading", { name: "#topic-origin" })).toBeVisible();
  await expect(page.getByText("failed in origin")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();

  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByRole("button", { name: "Retry" })).toHaveCount(0);
  await page.getByRole("link", { name: "# topic-destination", exact: true }).click();
  await expect(page.getByRole("heading", { name: "#topic-destination" })).toBeVisible();
  releaseRetry?.();
  await expect(page.getByText("failed in origin")).toHaveCount(0);
  await page.getByRole("link", { name: "# topic-origin", exact: true }).click();
  await expect(page.getByRole("heading", { name: "#topic-origin" })).toBeVisible();
  await expect(page.getByText("failed in origin")).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
});

test("switching topic filters discards delayed pagination from the previous filter", async ({
  page,
}) => {
  test.slow();
  const suffix = randomUUID().replaceAll("-", "").slice(0, 10);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Topic paging ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "topic-paging", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const releaseResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Release", channel_id: channel.id },
  });
  const designResponse = await page.request.post(`/api/workspaces/${workspace.id}/topics`, {
    data: { name: "Design", channel_id: channel.id },
  });
  expect(releaseResponse.ok()).toBe(true);
  expect(designResponse.ok()).toBe(true);
  const release = ((await releaseResponse.json()) as { topic: { id: string } }).topic;
  const design = ((await designResponse.json()) as { topic: { id: string } }).topic;

  for (let index = 0; index < 102; index += 1) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: `release page ${index}`, topic_id: release.id },
    });
    expect(response.ok()).toBe(true);
  }
  for (let index = 0; index < 2; index += 1) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: `design page ${index}`, topic_id: design.id },
    });
    expect(response.ok()).toBe(true);
  }

  let releaseDelayedPage: (() => void) | undefined;
  const releaseDelayed = new Promise<void>((resolve) => {
    releaseDelayedPage = resolve;
  });
  let releaseRequestSeen = false;
  await page.route(`**/api/channels/${channel.id}/messages?*`, async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.has("before_seq") && url.searchParams.get("topic_id") === release.id) {
      releaseRequestSeen = true;
      await releaseDelayed;
    }
    await route.continue();
  });

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  await page.getByRole("button", { name: "Filter by topic Release" }).first().click();
  await expect(page.getByText("Showing topic")).toContainText("Release");

  const scrollport = page.locator(".messages-scroll");
  await expect
    .poll(() => scrollport.evaluate((element) => element.scrollHeight - element.clientHeight))
    .toBeGreaterThan(0);
  await expect
    .poll(async () => {
      await scrollport.focus();
      await page.keyboard.press("Home");
      await scrollport.evaluate(
        () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
      );
      await scrollport.dispatchEvent("scroll");
      return releaseRequestSeen;
    })
    .toBe(true);

  await page.getByRole("button", { name: "Clear filter" }).click();
  await expect(page.getByText("Showing topic")).toHaveCount(0);
  await page.getByRole("button", { name: "Filter by topic Design" }).first().click();
  await expect(page.getByText("Showing topic")).toContainText("Design");
  await expect(page.getByText("design page 1")).toBeVisible();

  releaseDelayedPage?.();
  await page.waitForTimeout(300);
  await expect(page.getByText("release page 0")).toHaveCount(0);
  await expect(page.getByText("design page 0")).toBeVisible();
});
