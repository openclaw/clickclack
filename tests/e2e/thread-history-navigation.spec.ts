import { expect, test, type WebSocketRoute } from "@playwright/test";
import { deferred, longThread, expectInsideThread } from "./thread-fixture";

test("old reply search targets stay visible, return to search, and report deleted or forbidden targets", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const { root, replies, workspace } = await longThread(page, 128);
  await page.getByLabel("Search messages").fill('"Window reply 2"');
  await page.getByRole("button", { name: "Search", exact: true }).click();
  const result = page.locator(".search-result").filter({ hasText: "Window reply 2" });
  await expect(result).toHaveCount(1);
  let searches = 0;
  page.on("request", (request) => {
    if (request.url().includes("/api/search?")) searches++;
  });
  await result.click();
  const target = page.locator(`.reply[data-message-id="${replies[1].id}"]`);
  await expectInsideThread(target, page);
  await expect(page).toHaveURL(new RegExp(`/app/${workspace.route_id}/${root.route_id}$`));
  await page.getByRole("button", { name: "Back to search results" }).click();
  await expect(result).toBeVisible();
  expect(searches).toBe(0);

  await page.request.delete(`/api/messages/${replies[1].id}`);
  await result.click();
  await expect(page.getByRole("alert")).toContainText("no longer available");
  await page.getByRole("button", { name: "Back to search results" }).click();
  await page.route(`**/api/messages/${root.id}/thread?*`, (route) => {
    if (!new URL(route.request().url()).searchParams.has("around_seq")) return route.continue();
    return route.fulfill({ status: 403, json: { error: "Reply access denied" } });
  });
  await result.click();
  await expect(page.getByRole("alert")).toContainText("Reply access denied");
  await page.getByRole("button", { name: "Back to search results" }).click();
  await expect(result).toBeVisible();
  expect(searches).toBe(0);
});

test("a superseded around response cannot undo an explicit latest selection", async ({ page }) => {
  test.setTimeout(90_000);
  const { root, replies } = await longThread(page, 128);
  const quote = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
    data: { body: "Quote for delayed target", quoted_message_id: replies[1].id },
  });
  const quoted = (await quote.json()).message;
  const entered = deferred(),
    release = deferred(),
    delivered = deferred();
  await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
    if (!new URL(route.request().url()).searchParams.has("around_seq")) return route.continue();
    const response = await route.fetch();
    entered.resolve();
    await release.promise;
    await route.fulfill({ response });
    delivered.resolve();
  });
  try {
    await page
      .locator(`.reply[data-message-id="${quoted.id}"]`)
      .getByRole("button", { name: /Jump to quoted message/ })
      .click();
    await entered.promise;
    await page
      .locator(`.reply[data-message-id="${replies[40].id}"]`)
      .evaluate((node) => node.scrollIntoView({ block: "start" }));
    await page.getByRole("button", { name: "Jump to latest", exact: true }).click();
    await expectInsideThread(page.locator(`.reply[data-message-id="${quoted.id}"]`), page);
    release.resolve();
    await delivered.promise;
    await expect(page.locator(`.reply[data-message-id="${replies[1].id}"]`)).toHaveCount(0);
    await expectInsideThread(page.locator(`.reply[data-message-id="${quoted.id}"]`), page);
  } finally {
    release.resolve();
  }
});

test("disconnecting a held live edge releases its loading owner for reconnect", async ({
  page,
}) => {
  test.setTimeout(90_000);
  let socket: WebSocketRoute | undefined;
  let server: WebSocketRoute | undefined;
  let connections = 0;
  await page.routeWebSocket("**/api/realtime/ws?*", (ws) => {
    socket = ws;
    server = ws.connectToServer();
    connections++;
  });
  const { root, workspace } = await longThread(page, 102);
  const entered = deferred(),
    release = deferred(),
    delivered = deferred();
  let held = false;
  await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
    const query = new URL(route.request().url()).searchParams;
    if (held || !query.has("after_seq") || query.get("limit") !== "50") return route.continue();
    held = true;
    const response = await route.fetch();
    entered.resolve();
    await release.promise;
    await route.fulfill({ response });
    delivered.resolve();
  });
  try {
    const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
      data: { body: "Held before disconnect" },
    });
    const receipt = await response.json();
    await entered.promise;
    const previousConnections = connections;
    await socket!.close({ code: 1013, reason: "Test reconnect" });
    await server!.close();
    release.resolve();
    await delivered.promise;
    await expect.poll(() => connections).toBeGreaterThan(previousConnections);
    await expect(page.locator(`.reply[data-message-id="${receipt.message.id}"]`)).toBeAttached();
    const next = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
      data: { body: "After reconnect" },
    });
    const message = (await next.json()).message;
    await expectInsideThread(page.locator(`.reply[data-message-id="${message.id}"]`), page);
    await expect(page.getByRole("button", { name: "Load newer replies", exact: true })).toHaveCount(
      0,
    );
    await expect
      .poll(() =>
        page.evaluate((id) => localStorage.getItem(`clickclack:${id}:cursor`), workspace.id),
      )
      .not.toBeNull();
  } finally {
    release.resolve();
  }
});

for (const surface of ["main", "embed"] as const) {
  test(`${surface} reveals a retained editor after an explicit window jump`, async ({ page }) => {
    test.setTimeout(90_000);
    const { root, roots, replies, workspace } = await longThread(page, 128);
    if (surface === "embed")
      await page.goto(`/embed/thread/${workspace.route_id}/${root.route_id}`);
    await page.getByRole("button", { name: "Load older replies", exact: true }).click();
    const first = page.locator(`.reply[data-message-id="${replies[0].id}"]`);
    await first.getByRole("button", { name: "Edit message", exact: true }).click();
    await first.getByLabel("Edit message", { exact: true }).fill("Retained historical draft");
    await page.getByRole("button", { name: "Jump to latest", exact: true }).click();
    await expect(first).toHaveCount(0);
    await page
      .locator(`.reply[data-message-id="${replies.at(-1).id}"]`)
      .getByRole("button", { name: "Edit message", exact: true })
      .click();
    await expect(first.getByLabel("Edit message", { exact: true })).toHaveValue(
      "Retained historical draft",
    );
    await expectInsideThread(first, page);
    if (surface === "main") {
      // Conversation-scoped editing must also reveal the original root after a thread switch.
      const secondRoot = page.locator(`.message-row[data-message-id="${roots[1].id}"]`);
      await secondRoot.hover();
      await secondRoot.getByRole("button", { name: "Open thread", exact: true }).click();
      await page
        .locator(".reply")
        .getByRole("button", { name: "Edit message", exact: true })
        .click();
      await expect(first.getByLabel("Edit message", { exact: true })).toHaveValue(
        "Retained historical draft",
      );
      await expectInsideThread(first, page);
      await expect(page).toHaveURL(new RegExp(`/${root.route_id}$`));
    }
  });
}
