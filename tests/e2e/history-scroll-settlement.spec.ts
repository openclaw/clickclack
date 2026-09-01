import { expect, test, type APIRequestContext, type Locator, type Page } from "@playwright/test";
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
  return { path, route: `/app/${workspace.route_id}/${channel.route_id}`, targetID };
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
