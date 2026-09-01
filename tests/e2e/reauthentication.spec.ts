import { expect, test, type APIRequestContext, type WebSocketRoute } from "@playwright/test";
import { waitForAppReady } from "./app-ready";

async function signIn(request: APIRequestContext, name: string) {
  const magic = await request.post("/api/auth/magic/request", {
    data: { email: `${name}-${Date.now()}@example.com` },
  });
  const signedIn = await request.post("/api/auth/magic/consume", {
    data: { token: (await magic.json()).token },
  });
  expect(signedIn.ok()).toBe(true);
  return signedIn.json();
}

async function createConversation(request: APIRequestContext, name: string, body: string) {
  const headers = { "X-ClickClack-CSRF": "1" };
  const workspaceResponse = await request.post("/api/workspaces", {
    headers,
    data: { name: `${name} ${Date.now()}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = await workspaceResponse.json();
  const channelResponse = await request.post(`/api/workspaces/${workspace.id}/channels`, {
    headers,
    data: { name, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = await channelResponse.json();
  const messageResponse = await request.post(`/api/channels/${channel.id}/messages`, {
    headers,
    data: { body },
  });
  expect(messageResponse.ok()).toBe(true);
  const { message } = await messageResponse.json();
  const routed = await request.post(`/api/messages/${message.id}/route`, { headers });
  expect(routed.ok()).toBe(true);
  message.route_id = (await routed.json()).message.route_id;
  return { workspace, channel, message };
}

function conversationPath(
  surface: "chat" | "channel embed" | "thread embed",
  { workspace, channel, message }: Awaited<ReturnType<typeof createConversation>>,
) {
  return surface === "chat"
    ? `/app/${workspace.route_id}/${channel.route_id}`
    : surface === "channel embed"
      ? `/embed/channel/${workspace.route_id}/${channel.route_id}`
      : `/embed/thread/${workspace.route_id}/${message.route_id}`;
}

for (const surface of ["chat", "channel embed", "thread embed"] as const) {
  test(`${surface} returns to sign in when an open session is revoked without AbortSignal.any`, async ({
    page,
    request,
  }) => {
    await page.addInitScript(() => {
      delete (AbortSignal as Partial<typeof AbortSignal>).any;
    });
    const { user, token } = await signIn(
      page.request,
      `realtime-expiry-${surface.replaceAll(" ", "-")}`,
    );
    const fixture = await createConversation(
      page.request,
      "session-expiry",
      "Private content before session revocation",
    );
    const { workspace, channel, message } = fixture;
    const socketOpened = page.waitForEvent("websocket");
    await page.goto(conversationPath(surface, fixture));
    await expect(page.getByText(message.body, { exact: true })).toBeVisible();
    const socket = await socketOpened;
    await expect
      .poll(() =>
        page.evaluate(
          (key) => Boolean(localStorage.getItem(key)),
          `clickclack:${workspace.id}:cursor`,
        ),
      )
      .toBe(true);

    // Revoke through a separate client so the tab retains the invalid cookie.
    const closed = socket.waitForEvent("close");
    expect(
      (
        await request.post("/api/auth/logout", {
          headers: { Authorization: `Bearer ${token}` },
          data: {},
        })
      ).ok(),
    ).toBe(true);
    expect(
      (
        await request.post(`/api/channels/${channel.id}/messages`, {
          headers: { "X-ClickClack-User": user.id },
          data: { body: "After session revocation" },
        })
      ).ok(),
    ).toBe(true);
    await closed;
    await expect(page.getByRole("region", { name: "Sign in", exact: true })).toBeVisible();
    await expect(page.getByText(message.body, { exact: true })).not.toBeVisible();
  });

  test(`${surface} retires a deleted workspace while the account keeps its other workspace`, async ({
    page,
  }, testInfo) => {
    await signIn(page.request, `workspace-loss-${surface.replaceAll(" ", "-")}`);
    const retained = await createConversation(
      page.request,
      "remaining-workspace",
      "The remaining workspace is usable",
    );
    const body = "Content from the removed workspace";
    const removed = await createConversation(
      page.request,
      "removed-workspace",
      `${body}\n\n![Private image](https://clickclack-fixture.example/private.svg)`,
    );
    await page.route("https://clickclack-fixture.example/private.svg", (route) =>
      route.fulfill({
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="128" height="96"><rect width="128" height="96" fill="#4477bb"/></svg>',
      }),
    );
    let socket: WebSocketRoute | undefined;
    let upstream: WebSocketRoute | undefined;
    await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
      socket = client;
      upstream = client.connectToServer();
    });
    const loaded = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname ===
          (surface === "thread embed"
            ? `/api/messages/${removed.message.id}/thread`
            : `/api/channels/${removed.channel.id}/messages`) && response.ok(),
    );
    await page.goto(conversationPath(surface, removed));
    await loaded;
    await expect(page.getByText(body, { exact: true })).toBeVisible();
    await expect.poll(() => Boolean(socket)).toBe(true);
    await page.locator(".markdown").getByRole("img", { name: "Private image" }).click();
    const viewer = page.getByRole("dialog", { name: "Image viewer: Private image", exact: true });
    await expect(viewer).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("before-workspace-loss.png") });

    expect(
      (
        await page.request.delete(`/api/workspaces/${removed.workspace.id}`, {
          headers: { "X-ClickClack-CSRF": "1" },
        })
      ).status(),
    ).toBe(204);
    expect((await page.request.get("/api/me")).ok()).toBe(true);
    const available = (await (await page.request.get("/api/workspaces")).json()).workspaces;
    expect(available.map((workspace: { id: string }) => workspace.id)).toContain(
      retained.workspace.id,
    );
    expect(available.map((workspace: { id: string }) => workspace.id)).not.toContain(
      removed.workspace.id,
    );
    const denied = await page.request.get(
      `/api/realtime/events?workspace_id=${removed.workspace.id}&include_tail=true`,
    );
    expect([400, 403]).toContain(denied.status());
    // Deletion has no hub notification; reconnect to exercise the real access check.
    const disconnected = upstream!;
    await socket!.close({ code: 1008, reason: "Reconnect after workspace deletion" });
    await disconnected.close();
    await expect(viewer).not.toBeVisible();
    await expect(page.getByText(body, { exact: true })).not.toBeVisible();
    await expect(page.getByRole("region", { name: "Sign in", exact: true })).not.toBeVisible();
    if (surface === "chat") {
      await expect(page).toHaveURL(new RegExp(`/app/${retained.workspace.route_id}/`));
      await waitForAppReady(page);
      await expect(page.getByText(removed.channel.name, { exact: true })).not.toBeVisible();
    } else {
      await expect(
        page.getByRole("heading", {
          name: surface === "channel embed" ? "Channel unavailable" : "Thread unavailable",
        }),
      ).toBeVisible();
      await page.goto(conversationPath("chat", retained));
      await waitForAppReady(page);
    }
    await expect(page.getByText(retained.message.body, { exact: true })).toBeVisible();
    await page.getByLabel("Message body").fill("Still signed in after workspace loss");
    await page.getByLabel("Message body").press("Enter");
    await expect(
      page.getByText("Still signed in after workspace loss", { exact: true }),
    ).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("after-workspace-loss.png") });
  });

  test(`${surface} retains authorized messages when only realtime scope is denied`, async ({
    page,
  }) => {
    const fixture = await createConversation(
      page.request,
      "limited-realtime",
      "Message access remains authorized",
    );
    const created = await page.request.post(`/api/workspaces/${fixture.workspace.id}/bots`, {
      data: {
        display_name: "Limited realtime reader",
        scopes: [
          "profile:read",
          "workspaces:read",
          "channels:read",
          "messages:read",
          "threads:read",
          "dms:read",
        ],
      },
    });
    expect(created.ok()).toBe(true);
    const { bot_token } = await created.json();
    await page.context().setExtraHTTPHeaders({ Authorization: `Bearer ${bot_token.token}` });
    const denied = await page.request.get(
      `/api/realtime/events?workspace_id=${fixture.workspace.id}&include_tail=true`,
    );
    expect(denied.status()).toBe(403);
    expect((await page.request.get(`/api/channels/${fixture.channel.id}/messages`)).ok()).toBe(
      true,
    );
    expect((await page.request.get("/api/me")).ok()).toBe(true);
    const preflight = page.waitForResponse(
      (response) => new URL(response.url()).pathname === "/api/realtime/events",
    );
    await page.goto(conversationPath(surface, fixture));
    expect((await preflight).status()).toBe(403);
    await expect(page.getByRole("status").filter({ hasText: "realtime:read" })).toBeVisible();
    await expect(page.getByText(fixture.message.body, { exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "Sign in", exact: true })).not.toBeVisible();
  });
}

test("a late workspace denial cannot replace a newer workspace navigation", async ({ page }) => {
  await signIn(page.request, "workspace-navigation-race");
  const first = await createConversation(
    page.request,
    "remaining-one",
    "First remaining workspace",
  );
  const second = await createConversation(
    page.request,
    "remaining-two",
    "Second remaining workspace",
  );
  const available = (await (await page.request.get("/api/workspaces")).json()).workspaces;
  const target = available[0].id === first.workspace.id ? second : first;
  const removed = await createConversation(
    page.request,
    "removed-on-navigation",
    "Leave this workspace",
  );
  let socket: WebSocketRoute | undefined;
  let upstream: WebSocketRoute | undefined;
  await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
    socket = client;
    upstream = client.connectToServer();
  });
  const targetPath = conversationPath("chat", target);
  await page.goto(targetPath);
  await waitForAppReady(page);
  await page
    .getByRole("navigation", { name: "Workspaces" })
    .getByRole("link", { name: removed.workspace.name, exact: true })
    .click();
  await expect(page.getByText(removed.message.body, { exact: true })).toBeVisible();
  await waitForAppReady(page);

  let accessEntered!: () => void;
  let releaseAccess!: () => void;
  const accessStarted = new Promise<void>((resolve) => {
    accessEntered = resolve;
  });
  const accessReleased = new Promise<void>((resolve) => {
    releaseAccess = resolve;
  });
  await page.route(
    "**/api/workspaces",
    async (route) => {
      const response = await route.fetch();
      accessEntered();
      await accessReleased;
      await route.fulfill({ response });
    },
    { times: 1 },
  );
  let routeEntered!: () => void;
  let releaseRoute!: () => void;
  const routeStarted = new Promise<void>((resolve) => {
    routeEntered = resolve;
  });
  const routeReleased = new Promise<void>((resolve) => {
    releaseRoute = resolve;
  });
  await page.route(
    `**/api/routes/${target.workspace.route_id}/${target.channel.route_id}`,
    async (route) => {
      routeEntered();
      await routeReleased;
      await route.continue();
    },
  );
  try {
    expect(
      (
        await page.request.delete(`/api/workspaces/${removed.workspace.id}`, {
          headers: { "X-ClickClack-CSRF": "1" },
        })
      ).status(),
    ).toBe(204);
    const disconnected = upstream!;
    await socket!.close({ code: 1008, reason: "Recheck deleted workspace" });
    await disconnected.close();
    await accessStarted;
    const back = page.goBack();
    await routeStarted;
    const classified = page.waitForResponse(
      (response) => new URL(response.url()).pathname === "/api/workspaces",
    );
    releaseAccess();
    await (await classified).finished();
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    releaseRoute();
    await back;
    await expect(page).toHaveURL(new RegExp(`${targetPath}$`));
    await expect(page.getByText(target.message.body, { exact: true })).toBeVisible();
    await waitForAppReady(page);
  } finally {
    releaseAccess();
    releaseRoute();
  }
});

for (const denied of [
  { surface: "channel embed", status: 403, heading: "Channel unavailable" },
  { surface: "thread embed", status: 404, heading: "Thread not found" },
] as const) {
  test(`${denied.surface} still clears content when its resource refresh returns ${denied.status}`, async ({
    page,
  }) => {
    const fixture = await createConversation(
      page.request,
      "resource-loss",
      "Content before resource access changed",
    );
    let socket: WebSocketRoute | undefined;
    let upstream: WebSocketRoute | undefined;
    await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
      socket = client;
      upstream = client.connectToServer();
    });
    await page.goto(conversationPath(denied.surface, fixture));
    await expect(page.getByText(fixture.message.body, { exact: true })).toBeVisible();
    await expect.poll(() => Boolean(socket)).toBe(true);
    const resource =
      denied.surface === "channel embed"
        ? `/api/channels/${fixture.channel.id}/messages`
        : `/api/messages/${fixture.message.id}/thread`;
    await page.route(`**${resource}?*`, (route) =>
      route.fulfill({ status: denied.status, json: { error: "Resource access changed" } }),
    );
    const refreshed = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === resource && response.status() === denied.status,
    );
    const disconnected = upstream!;
    await socket!.close({ code: 4001, reason: "Refresh resource permissions" });
    await disconnected.close();
    await refreshed;
    await expect(page.getByRole("heading", { name: denied.heading, exact: true })).toBeVisible();
    await expect(page.getByText(fixture.message.body, { exact: true })).not.toBeVisible();
  });
}

for (const failure of [
  { status: 503, closeCode: 1013, message: "Session verification unavailable" },
]) {
  test(`realtime ${failure.status} preserves the session and resumes from its cursor`, async ({
    page,
  }) => {
    const workspace = (
      await (
        await page.request.post("/api/workspaces", {
          data: { name: `Reconnect ${failure.status} ${Date.now()}` },
        })
      ).json()
    ).workspace;
    const channel = (
      await (
        await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
          data: { name: "reconnect", kind: "public" },
        })
      ).json()
    ).channel;
    let socket: WebSocketRoute | undefined;
    let upstream: WebSocketRoute | undefined;
    let unavailable = false;
    await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
      socket = client;
      upstream = client.connectToServer();
    });
    await page.route("**/api/realtime/events?*", (route) =>
      unavailable
        ? route.fulfill({ status: failure.status, json: { error: failure.message } })
        : route.continue(),
    );
    await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
    await waitForAppReady(page);
    const cursorKey = `clickclack:${workspace.id}:cursor`;
    const cursor = await page.evaluate((key) => localStorage.getItem(key), cursorKey);
    expect(cursor).toBeTruthy();

    unavailable = true;
    const disconnected = upstream!;
    await socket!.close({ code: failure.closeCode, reason: failure.message });
    await disconnected.close();
    await expect(page.getByText(`Live updates: ${failure.message}`, { exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "Sign in", exact: true })).not.toBeVisible();
    expect((await page.request.get("/api/me")).ok()).toBe(true);

    const body = `Message queued during ${failure.status}`;
    expect(
      (
        await page.request.post(`/api/channels/${channel.id}/messages`, {
          data: { body },
        })
      ).ok(),
    ).toBe(true);
    expect(await page.evaluate((key) => localStorage.getItem(key), cursorKey)).toBe(cursor);
    unavailable = false;
    await expect(page.getByText(body, { exact: true })).toBeVisible();
    await waitForAppReady(page);
    await expect(
      page.getByText(`Live updates: ${failure.message}`, { exact: true }),
    ).not.toBeVisible();
  });
}

test("realtime session expiry returns to sign in", async ({ page }) => {
  await page.route("**/api/realtime/events?*", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: '{"error":"session revoked"}',
    }),
  );
  await page.goto("/app");
  await expect(page.getByRole("region", { name: "Sign in" })).toBeVisible();
  await expect(page.getByLabel("Message body")).not.toBeVisible();
});

test("a stalled realtime preflight times out and retries without losing its cursor", async ({
  page,
}) => {
  await page.clock.install();
  const workspace = (
    await (
      await page.request.post("/api/workspaces", {
        data: { name: `Preflight timeout ${Date.now()}` },
      })
    ).json()
  ).workspace;
  const channel = (
    await (
      await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
        data: { name: "preflight-timeout", kind: "public" },
      })
    ).json()
  ).channel;
  let socket: WebSocketRoute | undefined;
  let upstream: WebSocketRoute | undefined;
  let holdNext = false;
  let stalled = false;
  await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
    socket = client;
    upstream = client.connectToServer();
  });
  await page.route("**/api/realtime/events?*", (route) => {
    if (holdNext) {
      holdNext = false;
      stalled = true;
      return;
    }
    return route.continue();
  });
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  const cursorKey = `clickclack:${workspace.id}:cursor`;
  const cursor = await page.evaluate((key) => localStorage.getItem(key), cursorKey);
  expect(cursor).toBeTruthy();
  holdNext = true;
  const disconnected = upstream!;
  await socket!.close({ code: 1013, reason: "Retry after stalled preflight" });
  await disconnected.close();
  await expect.poll(() => stalled).toBe(true);
  const body = "Message queued while the preflight was stalled";
  expect(
    (await page.request.post(`/api/channels/${channel.id}/messages`, { data: { body } })).ok(),
  ).toBe(true);
  await page.clock.fastForward(30_000);
  await expect(page.getByRole("status").filter({ hasText: "Live updates:" })).toContainText(
    /timed out/i,
  );
  expect(await page.evaluate((key) => localStorage.getItem(key), cursorKey)).toBe(cursor);
  await page.clock.fastForward(1_200);
  await expect(page.getByText(body, { exact: true })).toBeVisible();
  await waitForAppReady(page);
});

test("signing in again cannot display the previous account's private content", async ({ page }) => {
  const suffix = Date.now();
  const workspace = (
    await (
      await page.request.post("/api/workspaces", { data: { name: `Reauth ${suffix}` } })
    ).json()
  ).workspace;
  const createChannel = async (name: string, kind: string) =>
    (
      await (
        await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
          data: { name, kind },
        })
      ).json()
    ).channel;
  const first = await createChannel(`private-${suffix}`, "private");
  const next = await createChannel(`next-${suffix}`, "public");
  const privateText = `Previous account private message ${suffix}`;
  expect(
    (
      await page.request.post(`/api/channels/${first.id}/messages`, { data: { body: privateText } })
    ).ok(),
  ).toBe(true);
  const replacement = (
    await (
      await page.request.post("/api/auth/magic/request", {
        data: { email: `replacement-${suffix}@example.com`, display_name: `Replacement ${suffix}` },
      })
    ).json()
  ).token;
  await page.goto(`/app/${workspace.route_id}/${first.route_id}`);
  await waitForAppReady(page);
  await expect(page.getByText(privateText, { exact: true })).toBeVisible();
  await page.route("**/api/routes/**", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: '{"error":"session expired"}',
    }),
  );
  await page.getByText(next.name, { exact: true }).click();
  await expect(page.getByRole("region", { name: "Sign in" })).toBeVisible();
  await page.unroute("**/api/routes/**");
  let notifyEntered!: () => void;
  const entered = new Promise<void>((resolve) => {
    notifyEntered = resolve;
  });
  let unblock!: () => void;
  const release = new Promise<void>((resolve) => {
    unblock = resolve;
  });
  await page.route("**/api/me", async (route) => {
    notifyEntered();
    await release;
    await route.continue();
  });
  try {
    if (!(await page.getByLabel("Sign-in token").isVisible()))
      await page.getByText("Have a sign-in token?").click();
    await page.getByLabel("Sign-in token").fill(replacement);
    await page.getByRole("button", { name: "Use token", exact: true }).click();
    await entered;
    await expect(page.getByText(privateText, { exact: true })).not.toBeVisible();
  } finally {
    unblock();
  }
  await expect(
    page.getByRole("button", { name: new RegExp(`Account settings for Replacement ${suffix}`) }),
  ).toBeVisible();
  await expect(page.getByText(first.name, { exact: true })).not.toBeVisible();
});
