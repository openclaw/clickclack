import { expect, test, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

type Channel = { id: string; name: string; route_id: string };

async function workspaceWithChannels(request: APIRequestContext, label: string, names: string[]) {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await request.post("/api/workspaces", {
    data: { name: `${label} ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channels: Record<string, Channel> = {};
  for (const name of names) {
    const response = await request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name, kind: "public" },
    });
    expect(response.ok()).toBe(true);
    channels[name] = ((await response.json()) as { channel: Channel }).channel;
  }
  const route = (name: string) => `/app/${workspace.route_id}/${channels[name].route_id}`;
  return { workspace, channels, route };
}

async function channelNames(request: APIRequestContext, workspaceID: string) {
  const response = await request.get(`/api/workspaces/${workspaceID}/channels`);
  expect(response.ok()).toBe(true);
  const { channels } = (await response.json()) as { channels: Channel[] };
  return channels.map((channel) => channel.name).sort();
}

test("owners delete a channel after reviewing what is lost", async ({ page }, testInfo) => {
  const { workspace, channels, route } = await workspaceWithChannels(page.request, "Delete proof", [
    "general",
    "doomed",
  ]);
  const posted = await page.request.post(`/api/channels/${channels.doomed.id}/messages`, {
    data: { body: "Synthetic history that goes with the channel" },
  });
  expect(posted.ok()).toBe(true);
  await page.goto(route("doomed"));
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Channel settings", exact: true }).click();
  await page.getByRole("button", { name: "Delete channel..." }).click();
  const dialog = page.getByRole("dialog", { name: "Delete #doomed?" });
  await expect(dialog.getByLabel("What will be deleted")).toContainText(/Messages\s*1/);
  const confirm = dialog.getByRole("button", { name: "Delete channel" });
  await expect(confirm).toBeDisabled();
  const input = dialog.getByLabel("Type doomed to confirm");
  await input.fill("doome");
  await expect(confirm).toBeDisabled();
  await input.fill("doomed");
  await expect(confirm).toBeEnabled();
  await page.screenshot({ path: testInfo.outputPath("delete-confirmation-before.png") });
  await confirm.click();

  await expect(dialog).toBeHidden();
  await expect(page.getByRole("status").filter({ hasText: "Deleted #doomed." })).toBeVisible();
  await expect(page).not.toHaveURL(new RegExp(channels.doomed.route_id));
  expect(await channelNames(page.request, workspace.id)).toEqual(["general"]);
  await page.screenshot({ path: testInfo.outputPath("delete-owner-after.png") });
});

test("people in a deleted channel are moved out with a notice", async ({ page, browser }) => {
  const { workspace, channels, route } = await workspaceWithChannels(
    page.request,
    "Watcher proof",
    ["general", "doomed"],
  );
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const memberID = execFileSync(
    "go",
    [
      "run",
      "./apps/api/cmd/clickclack",
      "admin",
      "user",
      "create",
      "--data",
      "./data/e2e",
      "--workspace",
      workspace.id,
      "--name",
      "Deletion Watcher",
      "--email",
      `deletion-watcher-${suffix}@example.com`,
    ],
    { cwd: process.cwd(), encoding: "utf8" },
  ).trim();
  const context = await browser.newContext({ extraHTTPHeaders: { "X-ClickClack-User": memberID } });
  try {
    const watcher = await context.newPage();
    await watcher.goto(route("doomed"));
    await waitForAppReady(watcher);
    await expect(
      watcher.getByRole("button", { name: "Channel settings", exact: true }),
    ).toHaveCount(0);

    const deleted = await page.request.delete(`/api/channels/${channels.doomed.id}`);
    expect(deleted.status()).toBe(204);

    await expect(
      watcher.getByRole("status").filter({ hasText: /#doomed was deleted/ }),
    ).toBeVisible();
    await expect(watcher).not.toHaveURL(new RegExp(channels.doomed.route_id));
  } finally {
    await context.close();
  }
});

test("the last channel cannot be deleted", async ({ page }) => {
  const { route } = await workspaceWithChannels(page.request, "Last channel proof", ["only"]);
  await page.goto(route("only"));
  await waitForAppReady(page);

  await page.getByRole("button", { name: "Channel settings", exact: true }).click();
  await page.getByRole("button", { name: "Delete channel..." }).click();
  const dialog = page.getByRole("dialog", { name: "Delete #only?" });
  await expect(dialog.getByRole("alert")).toContainText("at least one channel");
  await expect(dialog.getByLabel("Type only to confirm")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Delete channel" })).toBeDisabled();

  await dialog.getByRole("button", { name: "Archive instead" }).click();
  await expect(page.getByRole("dialog", { name: "Channel settings" })).toBeVisible();
});

test("a channel list captured before deletion cannot restore the sidebar entry", async ({
  page,
}) => {
  const { channels, workspace, route } = await workspaceWithChannels(page.request, "Delayed list", [
    "general",
    "doomed",
  ]);
  await page.goto(route("doomed"));
  await waitForAppReady(page);
  const entered = deferred(),
    release = deferred(),
    delivered = deferred();
  let held = false;
  await page.route(`**/api/workspaces/${workspace.id}/channels`, async (request) => {
    if (held || request.request().method() !== "GET") return request.continue();
    held = true;
    const response = await request.fetch();
    expect(
      (await response.json()).channels.some(
        (channel: Channel) => channel.id === channels.doomed.id,
      ),
    ).toBe(true);
    entered.resolve();
    await release.promise;
    await request.fulfill({ response });
    delivered.resolve();
  });
  try {
    // Metadata refresh awaits its read in the realtime queue. The owner's
    // deletion response removes the channel independently of that queue.
    expect(
      (
        await page.request.patch(`/api/channels/${channels.general.id}`, {
          data: { archived: true },
        })
      ).ok(),
    ).toBe(true);
    await entered.promise;
    await page.getByRole("button", { name: "Channel settings", exact: true }).click();
    await page.getByRole("button", { name: "Delete channel..." }).click();
    const dialog = page.getByRole("dialog", { name: "Delete #doomed?" });
    await dialog.getByLabel("Type doomed to confirm").fill("doomed");
    await dialog.getByRole("button", { name: "Delete channel" }).click();
    await expect(dialog).toBeHidden();
    release.resolve();
    await delivered.promise;
    await expect(page.locator(`.sidebar [href$="/${channels.doomed.route_id}"]`)).toHaveCount(0);
    await expect(page).not.toHaveURL(new RegExp(channels.doomed.route_id));
  } finally {
    release.resolve();
  }
});

for (const phase of ["first page", "next page"] as const) {
  test(`a delayed search ${phase} cannot restore deleted snippets`, async ({ page }) => {
    const { channels, workspace, route } = await workspaceWithChannels(
      page.request,
      "Delayed search",
      ["general", "doomed"],
    );
    for (let index = 0; index < (phase === "next page" ? 51 : 1); index += 1) {
      const posted = await page.request.post(`/api/channels/${channels.doomed.id}/messages`, {
        data: { body: `zebraproof deleted snippet ${index}` },
      });
      expect(posted.ok()).toBe(true);
    }
    await page.goto(route("doomed"));
    await waitForAppReady(page);
    const entered = deferred(),
      release = deferred(),
      delivered = deferred();
    await page.route("**/api/search?*", async (request) => {
      const response = await request.fetch();
      const snapshot = await response.json();
      const next = new URL(request.request().url()).searchParams.has("cursor");
      expect(snapshot.results).toHaveLength(phase === "next page" && !next ? 50 : 1);
      if (phase === "next page" && !next) {
        await request.fulfill({ response });
        return;
      }
      entered.resolve();
      await release.promise;
      await request.fulfill({ json: { ...snapshot, next_cursor: null } });
      delivered.resolve();
    });
    try {
      await page.getByLabel("Search messages").fill("zebraproof");
      await page.getByRole("button", { name: "Search", exact: true }).click();
      if (phase === "next page")
        await page.getByRole("button", { name: "Load more results" }).click();
      await entered.promise;
      await page.locator(`.sidebar [href$="/${channels.general.route_id}"]`).click();
      await expect(page).toHaveURL(new RegExp(channels.general.route_id));
      const deleted = await page.request.delete(`/api/channels/${channels.doomed.id}`);
      expect(deleted.status()).toBe(204);
      await expect
        .poll(() =>
          page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), workspace.id),
        )
        .not.toBeNull();
      await expect(page.locator(`.sidebar [href$="/${channels.doomed.route_id}"]`)).toHaveCount(0);
      release.resolve();
      await delivered.promise;
      const results = page.getByLabel("Search results");
      await expect(results.locator(".search-result")).toHaveCount(0);
      await expect(results.getByRole("status", { name: "Searching" })).toHaveCount(0);
    } finally {
      release.resolve();
    }
  });
}

test("open channel and thread embeds retire deleted history and composers", async ({
  page,
  context,
}, testInfo) => {
  const { channels, workspace } = await workspaceWithChannels(page.request, "Deleted embeds", [
    "general",
    "doomed",
  ]);
  const posted = await page.request.post(`/api/channels/${channels.doomed.id}/messages`, {
    data: { body: "zebraproof embedded root" },
  });
  const { message } = await posted.json();
  expect(
    (
      await page.request.post(`/api/messages/${message.id}/thread/replies`, {
        data: { body: "embedded reply" },
      })
    ).ok(),
  ).toBe(true);
  const routed = await page.request.post(`/api/messages/${message.id}/route`);
  const root = (await routed.json()).message;
  const threadPage = await context.newPage();
  await page.goto(`/embed/channel/${workspace.route_id}/${channels.doomed.route_id}`);
  await threadPage.goto(`/embed/thread/${workspace.route_id}/${root.route_id}`);
  await expect(page.getByLabel("Embedded channel")).toBeVisible();
  await expect(threadPage.getByLabel("Embedded thread")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("channel-embed-before.png") });
  await threadPage.screenshot({ path: testInfo.outputPath("thread-embed-before.png") });
  expect((await page.request.delete(`/api/channels/${channels.doomed.id}`)).status()).toBe(204);
  for (const embed of [page, threadPage]) {
    await expect(
      embed.getByRole("heading", { name: "Channel deleted", exact: true }),
    ).toBeVisible();
    await expect(embed.locator("[data-message-id], textarea")).toHaveCount(0);
  }
  await page.screenshot({ path: testInfo.outputPath("channel-embed-after.png") });
  await threadPage.screenshot({ path: testInfo.outputPath("thread-embed-after.png") });
});
