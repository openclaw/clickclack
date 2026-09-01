import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
  type WebSocketRoute,
} from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { pauseMessageFrames, settleScrollFrames } from "./message-frames";
import { deferred } from "./thread-fixture";

async function historyFixture(request: APIRequestContext) {
  const created = await request.post("/api/workspaces", {
    data: { name: `History settlement ${randomUUID()}` },
  });
  expect(created.ok()).toBe(true);
  const { workspace } = await created.json();
  const response = await request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "history" },
  });
  expect(response.ok()).toBe(true);
  const { channel } = await response.json();
  const path = `/api/channels/${channel.id}/messages`;
  let targetID = "";
  for (let start = 0; start < 160; start += 10) {
    const rows = await Promise.all(
      Array.from({ length: 10 }, async (_, index) => {
        const posted = await request.post(path, {
          data: { body: `History row ${start + index} ${"Readable history content. ".repeat(5)}` },
        });
        expect(posted.ok()).toBe(true);
        return (await posted.json()).message;
      }),
    );
    if (start === 100) targetID = rows[0].id;
  }
  expect(
    (
      await request.post(path, {
        data: { body: "Quote navigation", quoted_message_id: targetID },
      })
    ).ok(),
  ).toBe(true);
  expect(
    (
      await request.post(`/api/channels/${channel.id}/read`, {
        data: { seq: 161 },
      })
    ).ok(),
  ).toBe(true);
  return {
    path,
    route: `/app/${workspace.route_id}/${channel.route_id}`,
    embedRoute: `/embed/channel/${workspace.route_id}/${channel.route_id}`,
    targetID,
  };
}

async function deliverWheel(page: Page, viewport: Locator, deltaY: number) {
  // mouse.wheel acknowledges dispatch before the DOM listener necessarily runs.
  const event = await viewport.evaluateHandle((el) => ({
    delivered: new Promise<void>((resolve) => {
      el.addEventListener("wheel", () => resolve(), { once: true });
    }),
  }));
  await page.mouse.wheel(0, deltaY);
  await event.evaluate(({ delivered }) => delivered);
  await event.dispose();
}

let fixture: Awaited<ReturnType<typeof historyFixture>>;
test.beforeAll(async ({ request }) => {
  fixture = await historyFixture(request);
});

for (const surface of ["channel", "embedded channel"]) {
  test(`older paging preserves the visible row when its author group expands in the ${surface}`, async ({
    page,
  }) => {
    const { path, route: appRoute, embedRoute } = fixture;
    const entered = deferred(),
      release = deferred();
    let requests = 0;
    await page.route(`**${path}?*`, async (route) => {
      if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
      if (++requests === 1) {
        entered.resolve();
        await release.promise;
      }
      await route.continue();
    });
    await page.goto(surface === "channel" ? appRoute : embedRoute);
    await expect(page.locator(".markdown").filter({ hasText: "Quote navigation" })).toBeVisible();
    await settleScrollFrames(page);
    const viewport = page.locator(".messages-scroll");
    const olderPage = page.waitForResponse(
      (response) => response.url().includes(`${path}?before_seq=`) && response.ok(),
    );
    const anchor = await viewport.evaluate((el) => {
      el.scrollTop = 0;
      const row = el.querySelector<HTMLElement>("[data-message-id]")!;
      const captured = {
        id: row.dataset.messageId!,
        offset: row.getBoundingClientRect().top - el.getBoundingClientRect().top,
      };
      el.dispatchEvent(new Event("scroll", { bubbles: true }));
      return captured;
    });
    await entered.promise;
    await expect.poll(() => requests).toBe(1);
    release.resolve();
    await (await olderPage).finished();
    await settleScrollFrames(page);
    await expect(page.locator(".messages-history-pad")).toHaveCount(0);
    expect(requests).toBe(1);
    const row = page.locator(`[data-message-id="${anchor.id}"]`);
    await expect(row).toBeInViewport();
    const offset = await row.evaluate(
      (el) =>
        el.getBoundingClientRect().top -
        el.closest(".messages-scroll")!.getBoundingClientRect().top,
    );
    expect(Math.abs(offset - anchor.offset)).toBeLessThan(2);
  });
}

for (const interruption of ["loading", "restore", "quote"] as const) {
  test(`older paging continues after ${interruption === "quote" ? "a quote replaces its restore" : `wheel input during ${interruption}`}`, async ({
    page,
  }) => {
    const { path, route: appRoute, targetID } = fixture;
    const entered = deferred(),
      release = deferred();
    let requests = 0;
    let firstOlderID = "",
      secondOlderID = "";
    await page.route(`**${path}?*`, async (route) => {
      if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
      const requestNumber = ++requests;
      const response = await route.fetch();
      const id = (await response.json()).messages[0].id;
      if (requestNumber === 1) {
        firstOlderID = id;
        entered.resolve();
        await release.promise;
      }
      if (requestNumber === 2) secondOlderID = id;
      await route.fulfill({ response });
    });
    await page.goto(appRoute);
    await waitForAppReady(page);
    await settleScrollFrames(page);
    const viewport = page.locator(".messages-scroll");
    await viewport.hover();
    await page.mouse.wheel(0, -100000);
    await entered.promise;
    await settleScrollFrames(page);
    const paused = interruption !== "loading";
    if (paused) await pauseMessageFrames(page);
    try {
      if (paused) {
        release.resolve();
        await expect(page.locator(`[data-message-id="${firstOlderID}"]`)).toBeAttached();
        await expect(page.locator(".messages-history-pad")).toBeVisible();
      }
      if (interruption === "quote") {
        await page.getByRole("button", { name: /Jump to quoted message/ }).focus();
        await page.keyboard.press("Enter");
      } else {
        await deliverWheel(page, viewport, -100000);
        if (!paused) {
          await expect.poll(() => viewport.evaluate((el) => el.scrollTop)).toBeLessThan(2);
          await settleScrollFrames(page);
        }
      }
    } finally {
      release.resolve();
      if (paused) await page.evaluate(() => Reflect.get(window, "resumeMessageFrames")());
    }
    await settleScrollFrames(page);
    await expect(page.locator(".messages-history-pad")).toHaveCount(0);
    if (interruption === "quote") {
      await expect(page.locator(`[data-message-id="${targetID}"]`)).toBeInViewport();
    }
    if (interruption !== "loading") {
      await viewport.hover();
      await deliverWheel(page, viewport, 500);
      await expect.poll(() => viewport.evaluate((el) => el.scrollTop)).toBeGreaterThan(100);
      await deliverWheel(page, viewport, -100000);
    }
    await expect.poll(() => secondOlderID).not.toBe("");
    await expect(page.locator(`[data-message-id="${secondOlderID}"]`)).toBeAttached();
    await expect(page.locator(".messages-history-pad")).toHaveCount(0);
  });
}

test("a history resync preserves scrolling after a bottom capture", async ({ page }) => {
  const { path, route: appRoute } = fixture;
  let socket: WebSocketRoute | undefined, server: WebSocketRoute | undefined;
  await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
    socket = client;
    server = client.connectToServer();
  });
  await page.goto(appRoute);
  await waitForAppReady(page);
  await settleScrollFrames(page);
  const viewport = page.locator(".messages-scroll");
  await expect
    .poll(() => viewport.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeLessThan(2);
  const entered = deferred(),
    release = deferred();
  await page.route(`**${path}?*`, async (route) => {
    const response = await route.fetch();
    entered.resolve();
    await release.promise;
    await route.fulfill({ response });
  });
  const refreshed = page.waitForResponse(
    (response) => response.url().includes(`${path}?`) && response.ok(),
  );
  const upstream = server!;
  await socket!.close({ code: 4001, reason: "History resync regression" });
  await upstream.close();
  try {
    await entered.promise;
    await viewport.hover();
    await deliverWheel(page, viewport, -600);
    await expect
      .poll(() => viewport.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
      .toBeGreaterThan(300);
    await settleScrollFrames(page);
    const anchor = await viewport.evaluate((el) => {
      const top = el.getBoundingClientRect().top;
      const row = [...el.querySelectorAll<HTMLElement>("[data-message-id]")].find(
        (row) => row.getBoundingClientRect().bottom > top,
      )!;
      return { id: row.dataset.messageId!, offset: row.getBoundingClientRect().top - top };
    });
    release.resolve();
    await (await refreshed).finished();
    await settleScrollFrames(page);
    const row = page.locator(`[data-message-id="${anchor.id}"]`);
    await expect(row).toBeInViewport();
    const offset = await row.evaluate(
      (el) =>
        el.getBoundingClientRect().top -
        el.closest(".messages-scroll")!.getBoundingClientRect().top,
    );
    expect(Math.abs(offset - anchor.offset)).toBeLessThan(2);
  } finally {
    release.resolve();
  }
});

test("older paging preserves live following after an append during the request", async ({
  page,
  request,
}) => {
  const { path, route: appRoute } = fixture;
  const entered = deferred(),
    release = deferred();
  let requests = 0;
  await page.route(`**${path}?*`, async (route) => {
    if (!new URL(route.request().url()).searchParams.has("before_seq")) return route.continue();
    const response = await route.fetch();
    if (++requests === 1) {
      entered.resolve();
      await release.promise;
    }
    await route.fulfill({ response });
  });
  await page.goto(appRoute);
  await waitForAppReady(page);
  await settleScrollFrames(page);
  const viewport = page.locator(".messages-scroll");
  const olderPage = page.waitForResponse(
    (response) => response.url().includes(`${path}?before_seq=`) && response.ok(),
  );
  await viewport.hover();
  await deliverWheel(page, viewport, -100000);
  try {
    await entered.promise;
    await settleScrollFrames(page);
    await deliverWheel(page, viewport, 100000);
    await expect
      .poll(() => viewport.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
      .toBeLessThan(2);
    const posted = await request.post(path, {
      data: { body: "Live message while an older page is loading" },
    });
    expect(posted.ok()).toBe(true);
    const { message } = await posted.json();
    const row = page.locator(`[data-message-id="${message.id}"]`);
    await expect(row).toBeInViewport();
    release.resolve();
    await (await olderPage).finished();
    await settleScrollFrames(page);
    await expect(page.locator(".messages-history-pad")).toHaveCount(0);
    await expect(row).toBeInViewport();
    await expect
      .poll(() => viewport.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
      .toBeLessThan(2);
    expect(requests).toBe(1);
  } finally {
    release.resolve();
  }
});
