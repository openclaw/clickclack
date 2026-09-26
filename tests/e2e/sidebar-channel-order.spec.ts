import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

type Workspace = { id: string; route_id: string; name: string };

const channelIDs = new Map<string, string>();

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
    const { channel } = (await response.json()) as { channel: { id: string } };
    channelIDs.set(name, channel.id);
  }
  return { workspace, names };
}

async function accountChannelOrder(page: Page, workspaceID: string) {
  const response = await page.request.get("/api/me");
  if (!response.ok()) return null;
  const { user } = (await response.json()) as {
    user: { sidebar_preferences?: { channel_order?: Record<string, string[]> } };
  };
  return user.sidebar_preferences?.channel_order?.[workspaceID] ?? null;
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
  await waitForAppReady(page);

  await expect.poll(() => visibleChannelNames(page)).toEqual(names);

  const source = page.getByRole("button", { name: `Move #${names[2]}` });
  const target = page.getByRole("link", { name: `# ${names[0]}` }).locator("..");
  await source.dragTo(target, { targetPosition: { x: 40, y: 1 } });
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[2], names[0], names[1]]);

  await page.reload();
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[2], names[0], names[1]]);

  await page.getByRole("button", { name: `Move #${names[2]}` }).focus();
  await page.keyboard.press("ArrowDown");
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[0], names[2], names[1]]);
  await expect(page.getByText(`Moved #${names[2]} to position 2 of 3`)).toBeAttached();

  await page.getByRole("button", { name: `Move #${names[0]}` }).click();
  const moveMenu = page.getByRole("menu", { name: `Move #${names[0]}` });
  await expect(moveMenu).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(moveMenu).toBeHidden();
  await expect(page.getByRole("button", { name: `Move #${names[0]}` })).toBeFocused();

  await page.getByRole("button", { name: `Move #${names[0]}` }).click();
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
  await waitForAppReady(page);
  await expect
    .poll(() => visibleChannelNames(page))
    .toEqual([names[2], names[0], names[1], addedName]);
});

test("a reordered sidebar roams to a second browser context", async ({ browser, page }) => {
  const { workspace, names } = await createWorkspaceWithChannels(page, "Roaming channel order");
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user } = (await meResponse.json()) as { user: { id: string } };

  await page.goto(`/app/${workspace.route_id}`);
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(names);

  await page.getByRole("button", { name: `Move #${names[0]}` }).click();
  await page
    .getByRole("menu", { name: `Move #${names[0]}` })
    .getByRole("menuitem", { name: "Move down" })
    .click();
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[1], names[0], names[2]]);

  // The account copy is what the second context will read.
  await expect
    .poll(() => accountChannelOrder(page, workspace.id))
    .toEqual([names[1], names[0], names[2]].map((name) => channelIDs.get(name)));

  const secondContext = await browser.newContext({
    extraHTTPHeaders: { "X-ClickClack-User": user.id },
  });
  try {
    await secondContext.addCookies(await page.context().cookies());
    const secondPage = await secondContext.newPage();
    // A cold device has no cache at all; clearing makes that explicit.
    await secondPage.addInitScript(() => localStorage.clear());
    await secondPage.goto(`/app/${workspace.route_id}`);
    await waitForAppReady(secondPage);
    await expect
      .poll(() => visibleChannelNames(secondPage))
      .toEqual([names[1], names[0], names[2]]);
  } finally {
    await secondContext.close();
  }
});

test("channel ordering is isolated by workspace", async ({ page }) => {
  const first = await createWorkspaceWithChannels(page, "First channel order");
  const second = await createWorkspaceWithChannels(page, "Second channel order");
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user } = (await meResponse.json()) as { user: { id: string } };

  await page.goto(`/app/${first.workspace.route_id}`);
  await waitForAppReady(page);
  await page.getByRole("button", { name: `Move #${first.names[0]}` }).click();
  await page
    .getByRole("menu", { name: `Move #${first.names[0]}` })
    .getByRole("menuitem", { name: "Move down" })
    .click();
  await expect
    .poll(() => visibleChannelNames(page))
    .toEqual([first.names[1], first.names[0], first.names[2]]);

  await page.goto(`/app/${second.workspace.route_id}`);
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(second.names);
  const secondStorageKey = `clickclack:sidebar-channel-order:v1:${user.id}:${second.workspace.id}`;
  await expect
    .poll(() => page.evaluate((key) => localStorage.getItem(key), secondStorageKey))
    .toBeNull();

  await page.goto(`/app/${first.workspace.route_id}`);
  await waitForAppReady(page);
  await expect
    .poll(() => visibleChannelNames(page))
    .toEqual([first.names[1], first.names[0], first.names[2]]);
});

test("invalid saved channel ordering falls back to server order", async ({ page }) => {
  const { workspace, names } = await createWorkspaceWithChannels(page, "Invalid channel order");
  const meResponse = await page.request.get("/api/me");
  expect(meResponse.ok()).toBe(true);
  const { user } = (await meResponse.json()) as { user: { id: string } };
  const storageKey = `clickclack:sidebar-channel-order:v1:${user.id}:${workspace.id}`;
  await page.addInitScript((key) => {
    localStorage.setItem(key, "not-json");
  }, storageKey);

  await page.goto(`/app/${workspace.route_id}`);
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(names);
});

test("unavailable channel order storage keeps reordering functional and still roams", async ({
  page,
}) => {
  const { workspace, names } = await createWorkspaceWithChannels(page, "Blocked channel order");
  await page.addInitScript(() => {
    const blockedKeyPrefix = "clickclack:sidebar-channel-order:v1:";
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

  await page.goto(`/app/${workspace.route_id}`);
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(names);
  await page.getByRole("button", { name: `Move #${names[0]}` }).click();
  await page
    .getByRole("menu", { name: `Move #${names[0]}` })
    .getByRole("menuitem", { name: "Move down" })
    .click();
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[1], names[0], names[2]]);

  // The cache never took the write, so only the account can carry the order
  // across the reload. Wait for that write rather than racing the debounce.
  await expect
    .poll(() => accountChannelOrder(page, workspace.id))
    .toEqual([names[1], names[0], names[2]].map((name) => channelIDs.get(name)));

  await page.reload();
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual([names[1], names[0], names[2]]);
});

async function channelIDsForNames(targetPage: Page, workspaceID: string, names: string[]) {
  const response = await targetPage.request.get(`/api/workspaces/${workspaceID}/channels`);
  expect(response.ok()).toBe(true);
  const { channels } = (await response.json()) as { channels: { id: string; name: string }[] };
  const byName = new Map(channels.map((channel) => [channel.name, channel.id]));
  return names.map((name) => byName.get(name) as string);
}

function leadingChannelNames(targetPage: Page, count: number) {
  return targetPage
    .locator("#sidebar-channels-list a.channel .nav-label")
    .evaluateAll(
      (labels, take) => labels.slice(0, take).map((label) => label.textContent?.trim()),
      count,
    );
}

async function moveChannelDown(targetPage: Page, name: string) {
  await targetPage.getByRole("button", { name: `Move #${name}` }).click();
  await targetPage
    .getByRole("menu", { name: `Move #${name}` })
    .getByRole("menuitem", { name: "Move down" })
    .click();
}

function cachedChannelOrder(targetPage: Page, storageKey: string) {
  return targetPage.evaluate((key) => localStorage.getItem(key), storageKey);
}

async function currentUserID(targetPage: Page) {
  const response = await targetPage.request.get("/api/me");
  expect(response.ok()).toBe(true);
  const { user } = (await response.json()) as { user: { id: string } };
  return user.id;
}

// There is no UI for clearing a saved order, so this exercises the path another
// device takes: the account order is cleared through the API, and the browser
// that still holds a cached order has to drop it rather than restore it.
test("clearing the account order clears the cached one on the next load", async ({ page }) => {
  const { workspace, names } = await createWorkspaceWithChannels(page, "Cleared channel order");
  const userID = await currentUserID(page);
  const storageKey = `clickclack:sidebar-channel-order:v1:${userID}:${workspace.id}`;

  await page.goto(`/app/${workspace.route_id}`);
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(names);

  await moveChannelDown(page, names[0]);
  const reordered = [names[1], names[0], names[2]];
  await expect.poll(() => visibleChannelNames(page)).toEqual(reordered);
  await expect
    .poll(() => accountChannelOrder(page, workspace.id))
    .toEqual(reordered.map((name) => channelIDs.get(name)));
  await expect.poll(() => cachedChannelOrder(page, storageKey)).not.toBeNull();

  const cleared = await page.request.patch("/api/me", {
    data: { sidebar_preferences: { channel_order: { [workspace.id]: [] } } },
  });
  expect(cleared.ok()).toBe(true);
  // The cleared workspace stays in the response holding an empty list, which is
  // what tells the browser this is a clear and not a workspace that never saved.
  await expect.poll(() => accountChannelOrder(page, workspace.id)).toEqual([]);

  await page.reload();
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(names);
  await expect.poll(() => cachedChannelOrder(page, storageKey)).toBeNull();
});

// End to end this guards convergence: two tabs and the account all settle on the
// newest order after one tab reorders and the other leaves the workspace and
// returns. Note that the rail switch remounts the app and refetches the profile,
// so this exercise alone does not reach a stale boot snapshot; the rule that an
// account snapshot applies at most once per profile object and workspace is
// pinned by the unit test in apps/web/src/lib/channel-order.test.ts.
test("a second tab keeps the newer order when it returns to the workspace", async ({ page }) => {
  const first = await createWorkspaceWithChannels(page, "Two tab channel order");
  const second = await createWorkspaceWithChannels(page, "Two tab other order");

  await page.goto(`/app/${first.workspace.route_id}`);
  await waitForAppReady(page);
  await expect.poll(() => visibleChannelNames(page)).toEqual(first.names);

  // A saved order exists before the second tab boots, so that tab carries it in
  // its own account snapshot.
  await moveChannelDown(page, first.names[0]);
  const firstOrder = [first.names[1], first.names[0], first.names[2]];
  await expect.poll(() => visibleChannelNames(page)).toEqual(firstOrder);
  await expect
    .poll(() => accountChannelOrder(page, first.workspace.id))
    .toEqual(firstOrder.map((name) => channelIDs.get(name)));

  const peer = await page.context().newPage();
  try {
    await peer.goto(`/app/${first.workspace.route_id}`);
    await waitForAppReady(peer);
    await expect.poll(() => visibleChannelNames(peer)).toEqual(firstOrder);

    // The first tab reorders again. The second tab picks that up through the
    // cache the two share.
    await moveChannelDown(page, firstOrder[1]);
    const secondOrder = [first.names[1], first.names[2], first.names[0]];
    await expect.poll(() => visibleChannelNames(page)).toEqual(secondOrder);
    await expect.poll(() => visibleChannelNames(peer)).toEqual(secondOrder);
    await expect
      .poll(() => accountChannelOrder(page, first.workspace.id))
      .toEqual(secondOrder.map((name) => channelIDs.get(name)));

    // The second tab leaves the workspace and comes back without reloading, so
    // it still holds the account snapshot it booted with. That snapshot is now
    // older than the shared cache and must not replay over it, in this tab or,
    // by way of another cache write, in the first one.
    await peer.getByRole("link", { name: second.workspace.name, exact: true }).click();
    await expect.poll(() => visibleChannelNames(peer)).toEqual(second.names);
    await peer.getByRole("link", { name: first.workspace.name, exact: true }).click();
    await expect.poll(() => visibleChannelNames(peer)).toEqual(secondOrder);

    await expect.poll(() => visibleChannelNames(page)).toEqual(secondOrder);
    await expect
      .poll(() => accountChannelOrder(page, first.workspace.id))
      .toEqual(secondOrder.map((name) => channelIDs.get(name)));
  } finally {
    await peer.close();
  }
});

// The account copy stops at 500 ids. A device holding more than that must keep
// the positions past the cap through a roam, so this builds a workspace larger
// than the cap and checks the tail of the cached order after a reload.
const ROAMING_CAP = 500;
const OVER_CAP_CHANNELS = 520;

test("a local order longer than the roaming cap keeps its tail across a reload", async ({
  page,
}) => {
  test.slow();
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Over cap channel order ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as { workspace: Workspace };

  const created: string[] = [];
  for (let start = 0; start < OVER_CAP_CHANNELS; start += 20) {
    const batch = Array.from(
      { length: Math.min(20, OVER_CAP_CHANNELS - start) },
      (_, offset) => `c${String(start + offset).padStart(4, "0")}-${suffix}`,
    );
    const responses = await Promise.all(
      batch.map((name) =>
        page.request.post(`/api/workspaces/${workspace.id}/channels`, {
          data: { name, kind: "public" },
        }),
      ),
    );
    for (const response of responses) expect(response.ok()).toBe(true);
    created.push(...batch);
  }
  expect(created.length).toBe(OVER_CAP_CHANNELS);
  const ids = await channelIDsForNames(page, workspace.id, created);

  // A pre-existing local order, arranged on this device before anything roamed:
  // the last channel pulled to the front so the cached order is a permutation,
  // not the server's.
  const localOrder = [ids[ids.length - 1], ...ids.slice(0, ids.length - 1)];
  const userID = await currentUserID(page);
  const storageKey = `clickclack:sidebar-channel-order:v1:${userID}:${workspace.id}`;
  await page.addInitScript(({ key, value }) => localStorage.setItem(key, value), {
    key: storageKey,
    value: JSON.stringify(localOrder),
  });

  await page.goto(`/app/${workspace.route_id}`);
  await waitForAppReady(page);
  await expect
    .poll(() => leadingChannelNames(page, 2))
    .toEqual([created[created.length - 1], created[0]]);

  // One reorder is what sends the order to the account, capped on the way out.
  await moveChannelDown(page, created[created.length - 1]);
  const roamed = [localOrder[1], localOrder[0], ...localOrder.slice(2)];
  await expect
    .poll(() => accountChannelOrder(page, workspace.id))
    .toEqual(roamed.slice(0, ROAMING_CAP));

  await page.reload();
  await waitForAppReady(page);
  await expect
    .poll(() => leadingChannelNames(page, 2))
    .toEqual([created[0], created[created.length - 1]]);

  const cached = JSON.parse((await cachedChannelOrder(page, storageKey)) || "[]") as string[];
  expect(cached.length).toBe(OVER_CAP_CHANNELS);
  expect(cached).toEqual(roamed);
  expect(cached.slice(ROAMING_CAP)).toEqual(roamed.slice(ROAMING_CAP));
});
