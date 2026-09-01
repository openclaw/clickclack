import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { waitForAppReady } from "./app-ready";
import { deferred, openThread, threadFixture } from "./thread-fixture";

function clickclack(args: string[]): string {
  return execFileSync("go", ["run", "./apps/api/cmd/clickclack", ...args], {
    cwd: process.cwd(),
    encoding: "utf8",
  }).trim();
}

test("an obsolete route failure cannot poison a newer visit to the same channel", async ({
  page,
}) => {
  const response = await page.request.post("/api/workspaces", {
    data: { name: `Route failure ${Date.now()}` },
  });
  expect(response.ok()).toBe(true);
  const { workspace } = (await response.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channels: { id: string; route_id: string; name: string }[] = [];
  for (const name of ["route-a", "route-b", "route-c"]) {
    const created = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name, kind: "public" },
    });
    expect(created.ok()).toBe(true);
    channels.push((await created.json()).channel);
  }
  const [a, b, c] = channels;
  await page.goto(`/app/${workspace.route_id}/${b.route_id}`);
  await waitForAppReady(page);

  let releaseFailure!: () => void;
  const pendingFailure = new Promise<void>((resolve) => (releaseFailure = resolve));
  let firstRequest!: () => void;
  const firstHeld = new Promise<void>((resolve) => (firstRequest = resolve));
  let requests = 0;
  const routePath = `/api/routes/${workspace.route_id}/${a.route_id}`;
  await page.route(`**${routePath}`, async (route) => {
    if (++requests !== 1) {
      await route.continue();
      return;
    }
    firstRequest();
    await pendingFailure;
    await route.fulfill({ status: 503, json: { error: "Obsolete route failed" } });
  });

  await page.getByRole("link", { name: `# ${a.name}`, exact: true }).click();
  await firstHeld;
  await page.getByRole("link", { name: `# ${b.name}`, exact: true }).click();
  await page.getByRole("link", { name: `# ${a.name}`, exact: true }).click();
  await expect(page.getByRole("heading", { name: `#${a.name}`, exact: true })).toBeVisible();
  const failedResponse = page.waitForResponse(
    (result) => result.url().endsWith(routePath) && result.status() === 503,
  );
  releaseFailure();
  await failedResponse;
  // Let the rejected fetch and its reactive error handler settle before clicking.
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
  await waitForAppReady(page);
  await page.getByRole("link", { name: `# ${c.name}`, exact: true }).click();
  await expect(page.getByRole("heading", { name: `#${c.name}`, exact: true })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${c.route_id}$`));
});

for (const action of ["thread guidance", "recoverable route error"] as const) {
  test(`${action} leaves channel navigation available`, async ({ page }) => {
    const { workspace, channel } = await threadFixture(page);
    const created = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "after-notice", kind: "public" },
    });
    expect(created.ok()).toBe(true);
    const { channel: alternate } = (await created.json()) as {
      channel: { name: string; route_id: string };
    };
    const alternateLink = page.getByRole("link", { name: `# ${alternate.name}`, exact: true });

    if (action === "thread guidance") {
      await page.getByRole("button", { name: "Open a message thread", exact: true }).click();
      await expect(
        page.getByText("Pick a message to open its thread.", { exact: true }),
      ).toBeVisible();
    } else {
      await page.route(
        `**/api/routes/${workspace.route_id}/${alternate.route_id}`,
        (route) => route.fulfill({ status: 503, json: { error: "Temporary route failure" } }),
        { times: 1 },
      );
      await alternateLink.click();
      await expect(page.getByText("Temporary route failure", { exact: true })).toBeVisible();
      await page.getByRole("link", { name: `# ${channel.name}`, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`/${channel.route_id}$`));
    }

    await alternateLink.click();
    await expect(page).toHaveURL(new RegExp(`/${alternate.route_id}$`));
    await expect(
      page.getByRole("heading", { name: `#${alternate.name}`, exact: true }),
    ).toBeVisible();
  });
}

for (const action of [
  "search",
  "open pins",
  "close pins",
  "dismiss pins",
  "open profile",
  "close profile",
] as const) {
  test(`${action} restores its channel after interrupting a pending route`, async ({ page }) => {
    if (action === "open profile") {
      await page.addInitScript(() => {
        Object.assign(window, {
          clickclackDesktop: {
            integratedTitleBar: false,
            platform: "darwin",
            notify: async () => true,
            onNavigate: () => () => {},
            onQuickCompose: () => () => {},
            openSettings: () => {},
            setActiveRoute: (route: string) =>
              (document.documentElement.dataset.desktopRoute = route),
            setUnreadCount: () => {},
            signInWithGitHub: async () => true,
          },
        });
      });
    }
    const { workspace, channel, roots } = await threadFixture(page);
    const created = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "alternate-route", kind: "public" },
    });
    expect(created.ok()).toBe(true);
    const { channel: alternate } = (await created.json()) as {
      channel: { name: string; route_id: string };
    };
    const result = page.locator(`.search-result[data-result-id="${roots[0].id}"]`);
    if (action === "search") {
      await page.getByLabel("Search messages").fill(`"${roots[0].body}"`);
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await expect(result).toBeVisible();
    } else if (action === "close pins" || action === "dismiss pins") {
      await page.getByRole("button", { name: "Pinned items", exact: true }).click();
      await expect(page.getByLabel("Pinned messages pane", { exact: true })).toBeVisible();
    } else if (action === "close profile") {
      await page
        .locator("main.timeline")
        .getByRole("button", { name: /^View profile for/ })
        .first()
        .click();
      await expect(page.getByRole("button", { name: "Close profile", exact: true })).toBeVisible();
    }
    const routePath = `/api/routes/${workspace.route_id}/${alternate.route_id}`;
    const entered = deferred(),
      release = deferred(),
      delivered = deferred();
    await page.route(
      `**${routePath}`,
      async (route) => {
        const response = await route.fetch();
        entered.resolve();
        await release.promise;
        await route.fulfill({ response });
        delivered.resolve();
      },
      { times: 1 },
    );
    try {
      await page.getByRole("link", { name: `# ${alternate.name}`, exact: true }).click();
      await entered.promise;
      await expect(page).toHaveURL(new RegExp(`/${alternate.route_id}$`));
      await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
      if (action === "search") {
        await result.click();
        await expect(result).toHaveClass(/is-active/);
      } else if (action === "open pins" || action === "close pins" || action === "dismiss pins") {
        await page
          .getByRole("button", {
            name:
              action === "open pins"
                ? "Pinned items"
                : action === "close pins"
                  ? "Close pinned items"
                  : "Close pinned panel",
            exact: true,
          })
          .click();
        await expect(page.getByLabel("Pinned messages pane", { exact: true })).toHaveCount(
          action === "open pins" ? 1 : 0,
        );
      } else {
        if (action === "open profile") {
          await page
            .locator("main.timeline")
            .getByRole("button", { name: /^View profile for/ })
            .first()
            .click();
        } else {
          await page.getByRole("button", { name: "Close profile", exact: true }).click();
        }
        await expect(page.getByRole("button", { name: "Close profile", exact: true })).toHaveCount(
          action === "open profile" ? 1 : 0,
        );
      }
      await expect(page).toHaveURL(new RegExp(`/${channel.route_id}$`));
    } finally {
      release.resolve();
      await delivered.promise;
    }
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`/${channel.route_id}$`));
    if (action === "open profile") {
      await expect(page.getByRole("button", { name: "Close profile", exact: true })).toBeVisible();
      await expect(page.locator("html")).toHaveAttribute(
        "data-desktop-route",
        `/app/${workspace.route_id}/${channel.route_id}`,
      );
      await page.getByRole("button", { name: "Close profile", exact: true }).click();
      await openThread(page, roots[0].id);
      await expect(page).toHaveURL(new RegExp(`/${roots[0].route_id}$`));
      await page
        .locator("main.timeline")
        .getByRole("button", { name: /^View profile for/ })
        .first()
        .click();
      await expect(page.getByLabel("Profile pane", { exact: true })).toBeVisible();
      await expect(page).toHaveURL(new RegExp(`/${channel.route_id}$`));
      await expect(page.locator("html")).toHaveAttribute(
        "data-desktop-route",
        `/app/${workspace.route_id}/${channel.route_id}`,
      );
    }
    await page.getByRole("link", { name: `# ${alternate.name}`, exact: true }).click();
    await expect(page.getByRole("heading", { name: `#${alternate.name}` })).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`/${alternate.route_id}$`));
    await expect(page.getByRole("button", { name: "Close profile", exact: true })).toHaveCount(0);
  });
}

test("app routes restore channels, DMs, threads, fallbacks, and history navigation", async ({
  page,
}) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const workspaceID = workspace.id;
  const stamp = Date.now();

  const channelResponse = await page.request.post(`/api/workspaces/${workspaceID}/channels`, {
    data: { name: `route-${stamp}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };

  const rootResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: `route thread root ${stamp}` },
  });
  const { message: root } = (await rootResponse.json()) as {
    message: { id: string; route_id?: string; body: string };
  };
  await page.request.post(`/api/messages/${root.id}/thread/replies`, {
    data: { body: `route thread reply ${stamp}` },
  });
  const threadResponse = await page.request.get(`/api/messages/${root.id}/thread`);
  const { root: threadRoot } = (await threadResponse.json()) as {
    root: { id: string; route_id: string; body: string };
  };

  const secondUserID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceID,
    "--name",
    `Route User ${stamp}`,
    "--email",
    `route-${stamp}@example.com`,
  ]);
  const dmResponse = await page.request.post("/api/dms", {
    data: { workspace_id: workspaceID, member_ids: [secondUserID] },
  });
  expect(dmResponse.ok()).toBe(true);
  const { conversation } = (await dmResponse.json()) as {
    conversation: { id: string; route_id: string };
  };
  expect(conversation?.route_id).toBeTruthy();

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${channel.route_id}$`));

  const lateChannelResponse = await page.request.post(`/api/workspaces/${workspaceID}/channels`, {
    data: { name: `late-route-${stamp}`, kind: "public" },
  });
  const { channel: lateChannel } = (await lateChannelResponse.json()) as {
    channel: { id: string; route_id: string; name: string };
  };
  await page.goto(`/app/${workspace.route_id}/${lateChannel.route_id}`);
  await expect(page.getByRole("heading", { name: `#${lateChannel.name}` })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${lateChannel.route_id}$`));

  await page.goto("about:blank");
  await page.goto(`/app/${workspace.route_id}`);
  await expect(page.getByRole("heading", { name: `#${lateChannel.name}` })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${lateChannel.route_id}$`));

  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await expect(page.getByRole("heading", { name: /Route User/ })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${conversation.route_id}$`));

  const lateUserID = clickclack([
    "admin",
    "user",
    "create",
    "--data",
    "./data/e2e",
    "--workspace",
    workspaceID,
    "--name",
    `Late Route User ${stamp}`,
    "--email",
    `late-route-${stamp}@example.com`,
  ]);
  const lateDMResponse = await page.request.post("/api/dms", {
    data: { workspace_id: workspaceID, member_ids: [lateUserID] },
  });
  expect(lateDMResponse.ok()).toBe(true);
  const { conversation: lateConversation } = (await lateDMResponse.json()) as {
    conversation: { id: string; route_id: string };
  };
  expect(lateConversation?.route_id).toBeTruthy();
  await page.goto(`/app/${workspace.route_id}/${lateConversation.route_id}`);
  await expect(page.getByRole("heading", { name: /Late Route User/ })).toBeVisible();
  await expect(page).toHaveURL(
    new RegExp(`/app/${workspace.route_id}/${lateConversation.route_id}$`),
  );

  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await expect(page.getByRole("heading", { name: /Route User/ })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: /Route User/ })).toBeVisible();

  await page.goto(`/app/${workspace.route_id}/${threadRoot.route_id}`);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await expect(page.getByLabel("Thread pane")).toBeVisible();
  await expect(page.locator(".thread-root .markdown")).toContainText(root.body);
  await expect(page.locator(".reply .markdown")).toContainText(`route thread reply ${stamp}`);
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${threadRoot.route_id}$`));

  await page.goto(`/app/${workspaceID}/${channel.id}`);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${channel.route_id}$`));

  await page.goto(`/app/${workspaceID}/${conversation.id}`);
  await expect(page.getByRole("heading", { name: /Route User/ })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${conversation.route_id}$`));

  await page.goto(`/app/${workspaceID}/${root.id}`);
  await expect(page.getByLabel("Thread pane")).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${threadRoot.route_id}$`));

  await page.goto(`/app/${workspace.route_id}`);
  await expect(page).toHaveURL(/\/app\/T[A-Z0-9]{16}\/[CD][A-Z0-9]{16}$/);

  await page.goto(`/app/${workspaceID}/msg_missing_${stamp}`);
  await expect(page).toHaveURL(/\/app\/T[A-Z0-9]{16}\/[CD][A-Z0-9]{16}$/);
  await expect(page.getByText("Could not load ClickClack")).toHaveCount(0);
});

test("workspace settings discard bot state when navigating between workspaces", async ({
  page,
}) => {
  const stamp = Date.now();
  const createWorkspace = async (suffix: string) => {
    const response = await page.request.post("/api/workspaces", {
      data: {
        name: `Settings ${suffix} ${stamp}`,
        slug: `settings-${suffix.toLowerCase()}-${stamp}`,
      },
    });
    expect(response.ok()).toBe(true);
    const body = (await response.json()) as {
      workspace: { id: string; route_id: string };
    };
    return body.workspace;
  };
  const createBot = async (workspaceID: string, suffix: string) => {
    const displayName = `Settings ${suffix} Bot ${stamp}`;
    const response = await page.request.post(`/api/workspaces/${workspaceID}/bots`, {
      data: {
        display_name: displayName,
        handle: `settings-${suffix.toLowerCase()}-bot-${stamp}`,
        token_name: "e2e",
      },
    });
    expect(response.ok()).toBe(true);
    return displayName;
  };

  const workspaceA = await createWorkspace("A");
  const workspaceB = await createWorkspace("B");
  const botA = await createBot(workspaceA.id, "A");
  const botB = await createBot(workspaceB.id, "B");

  await page.goto(`/app/${workspaceA.route_id}/settings/bots`);
  await expect(page.getByText(botA, { exact: true })).toBeVisible();
  await expect(page.getByText(botB, { exact: true })).toHaveCount(0);

  const destination = `/app/${workspaceB.route_id}/settings/bots`;
  await page.evaluate((href) => {
    (
      window as typeof window & { __settingsNavigationMarker?: boolean }
    ).__settingsNavigationMarker = true;
    const link = document.createElement("a");
    link.href = href;
    link.dataset.workspaceNavigation = "true";
    link.textContent = "Switch workspace";
    document.body.append(link);
  }, destination);
  await page.locator("a[data-workspace-navigation]").dispatchEvent("click");

  await expect(page).toHaveURL(new RegExp(`${destination}$`));
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (window as typeof window & { __settingsNavigationMarker?: boolean })
            .__settingsNavigationMarker,
      ),
    )
    .toBe(true);
  await expect(page.getByText(botB, { exact: true })).toBeVisible();
  await expect(page.getByText(botA, { exact: true })).toHaveCount(0);
});

test("workspace ownership candidates include members after the first 200", async ({ page }) => {
  const workspacesResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspacesResponse.json()) as {
    workspaces: { id: string; route_id: string }[];
  };
  const workspace = workspaces[0];
  const requestedCursors: string[] = [];
  const member = (index: number, displayName = `Member ${index}`) => ({
    workspace_id: workspace.id,
    user: {
      id: `usr_transfer_${index}`,
      kind: "human",
      display_name: displayName,
      handle: `transfer-${index}`,
      avatar_url: "",
      created_at: "2026-07-12T00:00:00Z",
    },
    role: "member",
    joined_at: "2026-07-12T00:00:00Z",
  });

  await page.route(`**/api/workspaces/${workspace.id}/members?**`, async (route) => {
    const cursor = new URL(route.request().url()).searchParams.get("cursor") ?? "";
    requestedCursors.push(cursor);
    if (!cursor) {
      await route.fulfill({
        json: {
          members: Array.from({ length: 200 }, (_, index) => member(index)),
          next_cursor: "page-two",
          has_more: true,
          total_count: 201,
        },
      });
      return;
    }
    await route.fulfill({
      json: {
        members: [member(200, "Beyond Two Hundred")],
        has_more: false,
      },
    });
  });

  await page.goto(`/app/${workspace.route_id}/settings/overview`);
  const selector = page.getByRole("combobox", { name: "New workspace owner" });
  await expect(selector.getByRole("option", { name: "Beyond Two Hundred" })).toHaveCount(1);
  expect(requestedCursors).toEqual(["", "page-two"]);
});

test("uploading a workspace icon preserves pending profile edits", async ({ page }) => {
  const stamp = Date.now();
  const response = await page.request.post("/api/workspaces", {
    data: {
      name: `Icon Settings ${stamp}`,
      slug: `icon-settings-${stamp}`,
    },
  });
  expect(response.ok()).toBe(true);
  const { workspace } = (await response.json()) as {
    workspace: { id: string; route_id: string };
  };
  const nextName = `Edited Icon Settings ${stamp}`;
  const nextSlug = `edited-icon-settings-${stamp}`;

  await page.goto(`/app/${workspace.route_id}/settings/overview`);
  await page.getByLabel("Workspace name").fill(nextName);
  await page.getByLabel("Workspace slug").fill(nextSlug);
  const [uploadResult, updateResult] = await Promise.all([
    page.waitForResponse(
      (res) => res.url().endsWith("/api/uploads") && res.request().method() === "POST",
    ),
    page.waitForResponse(
      (res) =>
        res.url().endsWith(`/api/workspaces/${workspace.id}`) && res.request().method() === "PATCH",
    ),
    page.getByLabel("Workspace icon file").setInputFiles({
      name: "icon.png",
      mimeType: "image/png",
      buffer: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
        "base64",
      ),
    }),
  ]);
  expect(uploadResult.ok()).toBe(true);
  expect(updateResult.ok()).toBe(true);
  await expect(page.getByLabel("Workspace name")).toHaveValue(nextName);
  await expect(page.getByLabel("Workspace slug")).toHaveValue(nextSlug);
  const persisted = await page.request.get(`/api/workspaces/${workspace.id}`);
  expect(persisted.ok()).toBe(true);
  const body = (await persisted.json()) as {
    workspace: { name: string; slug: string; icon_url: string };
  };
  expect(body.workspace).toMatchObject({ name: nextName, slug: nextSlug });
  expect(body.workspace.icon_url).toMatch(/^\/api\/uploads\/upl_/);
});

test("uploading a workspace icon does not overwrite concurrent profile edits", async ({ page }) => {
  const stamp = Date.now();
  const response = await page.request.post("/api/workspaces", {
    data: {
      name: `Concurrent Icon ${stamp}`,
      slug: `concurrent-icon-${stamp}`,
    },
  });
  expect(response.ok()).toBe(true);
  const { workspace } = (await response.json()) as {
    workspace: { id: string; route_id: string; slug: string };
  };
  const concurrentName = `Concurrent Winner ${stamp}`;

  await page.goto(`/app/${workspace.route_id}/settings/overview`);
  const concurrentUpdate = await page.request.patch(`/api/workspaces/${workspace.id}`, {
    data: { name: concurrentName },
  });
  expect(concurrentUpdate.ok()).toBe(true);
  const [uploadResult, updateResult] = await Promise.all([
    page.waitForResponse(
      (res) => res.url().endsWith("/api/uploads") && res.request().method() === "POST",
    ),
    page.waitForResponse(
      (res) =>
        res.url().endsWith(`/api/workspaces/${workspace.id}`) && res.request().method() === "PATCH",
    ),
    page.getByLabel("Workspace icon file").setInputFiles({
      name: "icon.png",
      mimeType: "image/png",
      buffer: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
        "base64",
      ),
    }),
  ]);
  expect(uploadResult.ok()).toBe(true);
  expect(updateResult.ok()).toBe(true);
  const persisted = await page.request.get(`/api/workspaces/${workspace.id}`);
  expect(persisted.ok()).toBe(true);
  const body = (await persisted.json()) as {
    workspace: { name: string; slug: string; icon_url: string };
  };
  expect(body.workspace).toMatchObject({ name: concurrentName, slug: workspace.slug });
  expect(body.workspace.icon_url).toMatch(/^\/api\/uploads\/upl_/);
});

test("creating the first workspace enters the routed app state", async ({ page }) => {
  const requestedPaths: string[] = [];
  let workspaceCreated = false;
  const workspace = {
    id: "wsp_empty_flow",
    route_id: "T01KR3EMPTYFLOW12",
    name: "Fresh Workspace",
    slug: "fresh-workspace",
    created_at: "2026-05-11T00:00:00Z",
  };

  await page.route("**/api/workspaces", async (route) => {
    requestedPaths.push(new URL(route.request().url()).pathname);
    if (route.request().method() === "GET") {
      await route.fulfill({ json: { workspaces: workspaceCreated ? [workspace] : [] } });
      return;
    }
    if (route.request().method() === "POST") {
      workspaceCreated = true;
      await route.fulfill({
        json: {
          workspace,
        },
      });
      return;
    }
    await route.fallback();
  });
  await page.route("**/api/workspaces/wsp_empty_flow/channels", async (route) => {
    requestedPaths.push(new URL(route.request().url()).pathname);
    await route.fulfill({ json: { channels: [] } });
  });
  await page.route("**/api/dms**", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("workspace_id") !== "wsp_empty_flow") {
      await route.fallback();
      return;
    }
    requestedPaths.push(`${url.pathname}?workspace_id=${url.searchParams.get("workspace_id")}`);
    await route.fulfill({ json: { conversations: [] } });
  });

  await page.goto("/app");
  await page.getByRole("button", { name: "Create workspace" }).click();
  await page.getByRole("textbox", { name: "Workspace name" }).fill("Fresh Workspace");
  await page.getByRole("textbox", { name: "Workspace name" }).press("Enter");

  await expect(page).toHaveURL(/\/app\/T01KR3EMPTYFLOW12$/);
  await expect(page.getByText("Fresh Workspace").first()).toBeVisible();
  await expect(page.getByPlaceholder("Pick a channel to start")).toBeVisible();
  await expect
    .poll(() => requestedPaths.includes("/api/workspaces/wsp_empty_flow/channels"))
    .toBe(true);
  await expect
    .poll(() => requestedPaths.includes("/api/dms?workspace_id=wsp_empty_flow"))
    .toBe(true);
});
