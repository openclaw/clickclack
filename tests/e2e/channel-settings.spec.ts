import { expect, test, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { createGeneralChannel } from "./channel-fixture";

function admin(args: string[]) {
  return execFileSync(
    "go",
    ["run", "./apps/api/cmd/clickclack", "admin", ...args, "--data", "./data/e2e"],
    {
      cwd: process.cwd(),
      encoding: "utf8",
    },
  ).trim();
}

async function archivedAt(request: APIRequestContext, workspaceID: string, channelID: string) {
  const response = await request.get(`/api/workspaces/${workspaceID}/channels`);
  expect(response.ok()).toBe(true);
  const { channels } = (await response.json()) as {
    channels: { id: string; archived_at?: string | null }[];
  };
  return channels.find((channel) => channel.id === channelID)?.archived_at ?? null;
}

test("owners archive and restore a channel from channel settings", async ({ page }, testInfo) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Owner archive proof");
  await page.goto(route);
  await waitForAppReady(page);

  const dialog = page.getByRole("dialog", { name: "Channel settings", exact: true });
  await page.getByRole("button", { name: "Channel settings", exact: true }).click();
  await expect(dialog).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("owner-channel-settings.png") });
  await dialog.getByRole("button", { name: "Archive channel" }).click();
  await dialog.getByRole("button", { name: "Archive channel" }).click();
  await expect(dialog).toBeHidden();
  await expect.poll(() => archivedAt(page.request, workspace.id, channel.id)).not.toBeNull();

  await page.getByRole("button", { name: "Channel settings", exact: true }).click();
  await dialog.getByRole("button", { name: "Restore channel" }).click();
  await expect(dialog).toBeHidden();
  await expect.poll(() => archivedAt(page.request, workspace.id, channel.id)).toBeNull();
});

test("moderators are not offered channel settings the server rejects", async ({
  page,
  browser,
}, testInfo) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Moderator archive proof");
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user: owner } = (await meResponse.json()) as { user: { id: string } };
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const moderatorID = admin([
    "user",
    "create",
    "--name",
    "Channel Settings Moderator",
    "--email",
    `channel-settings-moderator-${suffix}@example.com`,
  ]);
  admin([
    "member",
    "add",
    "--workspace",
    workspace.id,
    "--created-by",
    owner.id,
    "--user",
    moderatorID,
    "--role",
    "moderator",
  ]);

  const context = await browser.newContext({
    extraHTTPHeaders: { "X-ClickClack-User": moderatorID },
  });
  try {
    const moderatorPage = await context.newPage();
    await moderatorPage.goto(route);
    await waitForAppReady(moderatorPage);
    await expect(moderatorPage.getByRole("button", { name: "Pinned items" })).toBeVisible();
    await expect(
      moderatorPage.getByRole("button", { name: "Channel settings", exact: true }),
    ).toHaveCount(0);
    await moderatorPage.screenshot({ path: testInfo.outputPath("moderator-channel-settings.png") });

    const archive = await moderatorPage.request.patch(`/api/channels/${channel.id}`, {
      data: { archived: true },
    });
    expect(archive.status()).toBe(403);
    expect(await archivedAt(page.request, workspace.id, channel.id)).toBeNull();
  } finally {
    await context.close();
  }
});
