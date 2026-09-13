import { expect, test, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

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

test("owners delete a channel after reviewing what is lost", async ({ page }) => {
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
  await confirm.click();

  await expect(dialog).toBeHidden();
  await expect(page.getByRole("status").filter({ hasText: "Deleted #doomed." })).toBeVisible();
  await expect(page).not.toHaveURL(new RegExp(channels.doomed.route_id));
  expect(await channelNames(page.request, workspace.id)).toEqual(["general"]);
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
