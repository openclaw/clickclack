import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";

test("sidebar sections collapse independently and persist per workspace", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Sidebar Workspace ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const activeChannelResponse = await page.request.post(
    `/api/workspaces/${workspace.id}/channels`,
    { data: { name: `active-${suffix}`, kind: "public" } },
  );
  expect(activeChannelResponse.ok()).toBe(true);
  const { channel: activeChannel } = (await activeChannelResponse.json()) as {
    channel: { route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `sidebar-proof-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: "Sidebar Proof Bot",
      handle: `sidebar-proof-${suffix}`,
      token_name: "e2e",
      scopes: ["bot:write"],
    },
  });
  expect(botResponse.ok()).toBe(true);
  const { bot_token: botToken } = (await botResponse.json()) as {
    bot_token: { token: string };
  };
  for (let index = 0; index < 101; index++) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      headers: { Authorization: `Bearer ${botToken.token}` },
      data: { body: `sidebar unread ${index}` },
    });
    expect(response.ok()).toBe(true);
  }

  await page.goto(`/app/${workspace.route_id}/${activeChannel.route_id}`);
  const channels = page.getByRole("button", { name: "Channels", exact: true });
  const directMessages = page.getByRole("button", { name: "Direct messages", exact: true });
  const people = page.getByRole("button", { name: "People", exact: true });
  await expect(channels).toHaveAttribute("aria-controls", "sidebar-channels-list");
  await expect(directMessages).toHaveAttribute("aria-controls", "sidebar-direct-messages-list");
  await expect(people).toHaveAttribute("aria-controls", "sidebar-people-list");

  for (const toggle of [channels, directMessages, people]) {
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  }
  await channels.click();
  await expect(page.locator("#sidebar-channels-list")).toBeHidden();
  await expect(page.locator("#sidebar-direct-messages-list")).toBeVisible();
  await expect(channels.locator("..").getByLabel("101 unread", { exact: true })).toHaveText("99+");
  await directMessages.click();
  await people.click();

  await page.getByRole("button", { name: "Create channel" }).click();
  await expect(
    page.locator(".profile-modal").getByRole("heading", { name: "Create channel" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Start direct message" }).click();
  await expect(
    page.locator(".profile-modal").getByRole("heading", { name: "Start a DM" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");

  await page.reload();
  for (const toggle of [channels, directMessages, people]) {
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
  }
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await expect(channels).toHaveAttribute("aria-expanded", "false");

  const secondResponse = await page.request.post("/api/workspaces", {
    data: { name: `Second Sidebar Workspace ${suffix}` },
  });
  expect(secondResponse.ok()).toBe(true);
  const { workspace: second } = (await secondResponse.json()) as {
    workspace: { route_id: string };
  };
  await page.goto(`/app/${second.route_id}`);
  for (const toggle of [channels, directMessages, people]) {
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  }

  await page.goto(`/app/${workspace.route_id}`);
  await page.evaluate((workspaceID) => {
    localStorage.setItem(`clickclack:sidebar-sections:v1:${workspaceID}`, "not-json");
  }, workspace.id);
  await page.reload();
  for (const toggle of [channels, directMessages, people]) {
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Toggle navigation" }).click();
  await channels.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#sidebar-channels-list")).toBeHidden();

  await page.addInitScript(() => {
    const blockedKeyPrefix = "clickclack:sidebar-sections:v1:";
    const getItem = Storage.prototype.getItem;
    const setItem = Storage.prototype.setItem;
    Storage.prototype.getItem = function (key: string) {
      if (key.startsWith(blockedKeyPrefix)) throw new Error("blocked storage");
      return getItem.call(this, key);
    };
    Storage.prototype.setItem = function (key: string, value: string) {
      if (key.startsWith(blockedKeyPrefix)) throw new Error("blocked storage");
      return setItem.call(this, key, value);
    };
  });
  await page.reload();
  await page.getByRole("button", { name: "Toggle navigation" }).click();
  await expect(channels).toHaveAttribute("aria-expanded", "true");
  await channels.focus();
  await page.keyboard.press("Enter");
  await expect(channels).toHaveAttribute("aria-expanded", "false");
});
