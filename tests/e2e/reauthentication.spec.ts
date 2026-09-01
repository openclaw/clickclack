import { expect, test, type WebSocketRoute } from "@playwright/test";
import { waitForAppReady } from "./app-ready";

for (const surface of ["chat", "channel embed", "thread embed"] as const) {
  test(`${surface} returns to sign in when an open session is revoked`, async ({
    page,
    request,
  }) => {
    const magic = await page.request.post("/api/auth/magic/request", {
      data: { email: `realtime-expiry-${surface.replaceAll(" ", "-")}-${Date.now()}@example.com` },
    });
    const signedIn = await page.request.post("/api/auth/magic/consume", {
      data: { token: (await magic.json()).token },
    });
    expect(signedIn.ok()).toBe(true);
    const { user, token } = await signedIn.json();
    const headers = { "X-ClickClack-CSRF": "1" };
    const workspace = (
      await (
        await page.request.post("/api/workspaces", {
          headers,
          data: { name: `Session expiry ${Date.now()}` },
        })
      ).json()
    ).workspace;
    const channel = (
      await (
        await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
          headers,
          data: { name: "session-expiry", kind: "public" },
        })
      ).json()
    ).channel;
    const message = (
      await (
        await page.request.post(`/api/channels/${channel.id}/messages`, {
          headers,
          data: { body: "Private content before session revocation" },
        })
      ).json()
    ).message;
    if (surface === "thread embed") {
      const routed = await page.request.post(`/api/messages/${message.id}/route`, { headers });
      expect(routed.ok()).toBe(true);
      message.route_id = (await routed.json()).message.route_id;
    }
    const target =
      surface === "chat"
        ? `/app/${workspace.route_id}/${channel.route_id}`
        : surface === "channel embed"
          ? `/embed/channel/${workspace.route_id}/${channel.route_id}`
          : `/embed/thread/${workspace.route_id}/${message.route_id}`;
    const socketOpened = page.waitForEvent("websocket");
    await page.goto(target);
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
}

for (const failure of [
  { status: 403, closeCode: 1008, message: "Workspace access revoked" },
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
