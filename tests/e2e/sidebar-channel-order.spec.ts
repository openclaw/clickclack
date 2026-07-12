import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";

type Workspace = { id: string; route_id: string };

async function createWorkspaceWithChannels(
  page: Page,
  label: string,
): Promise<{ workspace: Workspace; names: string[] }> {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `${label} ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as { workspace: Workspace };
  const names = [`aa-order-${suffix}`, `mm-order-${suffix}`, `zz-order-${suffix}`];
  for (const name of names) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name, kind: "public" },
    });
    expect(response.ok()).toBe(true);
  }
  return { workspace, names };
}

function visibleChannelNames(page: Page) {
  return page
    .locator("#sidebar-channels-list a.channel .nav-label")
    .evaluateAll((labels) => labels.map((label) => label.textContent?.trim()));
}

test("channel ordering supports drag, keyboard, touch actions, and collapsed sections", async ({
  page,
}) => {
  const { workspace, names } = await createWorkspaceWithChannels(page, "Channel order");
  await page.goto(`/app/${workspace.route_id}`);

  await expect.poll(() => visibleChannelNames(page)).toEqual(names);

  const source = page.getByRole("button", { name: `Move #${names[2]}` });
  const target = page.getByRole("link", { name: `# ${names[0]}` }).locator("..");
  await source.dragTo(target, { targetPosition: { x: 40, y: 1 } });
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[2], names[0], names[1]]);

  await page.reload();
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[2], names[0], names[1]]);

  await page.getByRole("button", { name: `Move #${names[2]}` }).focus();
  await page.keyboard.press("ArrowDown");
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[0], names[2], names[1]]);
  await expect(page.getByText(`Moved #${names[2]} to position 2 of 3`)).toBeAttached();

  await page.getByRole("button", { name: `Move #${names[0]}` }).click();
  const moveMenu = page.getByRole("menu", { name: `Move #${names[0]}` });
  await expect(moveMenu).toBeVisible();
  await moveMenu.getByRole("menuitem", { name: "Move down" }).click();
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[2], names[0], names[1]]);

  const channelsToggle = page.getByRole("button", { name: "Channels", exact: true });
  await channelsToggle.click();
  await expect(channelsToggle).toHaveAttribute("aria-expanded", "false");
  await expect(page.getByRole("button", { name: /^Move #/ })).toHaveCount(0);
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[0]]);

  await channelsToggle.click();
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[2], names[0], names[1]]);

  const addedName = `bb-order-${randomUUID().replaceAll("-", "").slice(0, 12)}`;
  const addedResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: addedName, kind: "public" },
  });
  expect(addedResponse.ok()).toBe(true);
  await page.reload();
  await expect
    .poll(() => visibleChannelNames(page))
    .toEqual([names[2], names[0], names[1], addedName]);
});

test("channel ordering is isolated by workspace", async ({ page }) => {
  const first = await createWorkspaceWithChannels(page, "First channel order");
  const second = await createWorkspaceWithChannels(page, "Second channel order");
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user } = (await meResponse.json()) as { user: { id: string } };

  await page.goto(`/app/${first.workspace.route_id}`);
  await page.getByRole("button", { name: `Move #${first.names[0]}` }).click();
  await page
    .getByRole("menu", { name: `Move #${first.names[0]}` })
    .getByRole("menuitem", { name: "Move down" })
    .click();
  await expect
    .poll(() => visibleChannelNames(page))
    .toEqual([first.names[1], first.names[0], first.names[2]]);

  await page.goto(`/app/${second.workspace.route_id}`);
  await expect.poll(() => visibleChannelNames(page)).toEqual(second.names);
  const secondStorageKey = `clickclack:sidebar-channel-order:v1:${user.id}:${second.workspace.id}`;
  await expect
    .poll(() => page.evaluate((key) => localStorage.getItem(key), secondStorageKey))
    .toBeNull();

  await page.goto(`/app/${first.workspace.route_id}`);
  await expect
    .poll(() => visibleChannelNames(page))
    .toEqual([first.names[1], first.names[0], first.names[2]]);
});
