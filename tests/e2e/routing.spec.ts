import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";

function clickclack(args: string[]): string {
  return execFileSync("go", ["run", "./apps/api/cmd/clickclack", ...args], {
    cwd: process.cwd(),
    encoding: "utf8",
  }).trim();
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
  const { conversation } = (await dmResponse.json()) as {
    conversation: { id: string; route_id: string };
  };

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
  const { conversation: lateConversation } = (await lateDMResponse.json()) as {
    conversation: { id: string; route_id: string };
  };
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
