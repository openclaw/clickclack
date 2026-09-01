import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { settleScrollFrames } from "./message-frames";
import { deferred } from "./thread-fixture";

async function conversationFixture(
  page: Page,
  kind: "channel" | "dm",
  singleAuthor = false,
  readSeq = 160,
  messageCount = 160,
) {
  const suffix = randomUUID().slice(0, 8);
  const created = await page.request.post("/api/workspaces", {
    data: { name: `Message windows ${suffix}` },
  });
  expect(created.ok()).toBe(true);
  const { workspace } = await created.json();
  const memberResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: `Window reader ${suffix}`,
      handle: `reader-${suffix}`,
      initial_token: false,
    },
  });
  expect(memberResponse.ok()).toBe(true);
  const { bot } = await memberResponse.json();
  let conversation;
  if (kind === "channel") {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: `windows-${suffix}` },
    });
    expect(response.ok()).toBe(true);
    conversation = (await response.json()).channel;
  } else {
    const direct = await page.request.post("/api/dms", {
      data: { workspace_id: workspace.id, member_ids: [bot.id] },
    });
    expect(direct.ok()).toBe(true);
    conversation = (await direct.json()).conversation;
  }
  const path = `/api/${kind === "channel" ? "channels" : "dms"}/${conversation.id}`;
  const messages: Message[] = [];
  for (let seq = 1; seq <= messageCount; seq++) {
    const body =
      seq === 11 ? "windowtarget Alpha" : seq === 131 ? "windowtarget Bravo" : `Row ${seq}`;
    const response = await page.request.post(`${path}/messages`, {
      headers: !singleAuthor && seq % 2 === 0 ? { "X-ClickClack-User": bot.id } : {},
      data: { body },
    });
    expect(response.ok()).toBe(true);
    messages.push((await response.json()).message);
  }
  expect((await page.request.post(`${path}/read`, { data: { seq: readSeq } })).ok()).toBe(true);
  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await waitForAppReady(page);
  await expect(
    page.locator(
      `.message-row[data-message-id="${messages[Math.min(readSeq, messageCount - 1)].id}"]`,
    ),
  ).toBeVisible();
  if (messageCount >= 131) {
    await page.getByLabel("Search messages").fill("windowtarget");
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await expect(page.locator(".search-result")).toHaveCount(2);
  }
  return { path, messages, workspace, conversation, bot };
}

async function expectInTimeline(page: Page, message: Message) {
  await expect(page.locator(`.message-row[data-message-id="${message.id}"]`)).toBeInViewport({
    ratio: 0.5,
  });
}

test("a returning empty snapshot cannot erase the first confirmed send", async ({ page }) => {
  const created = await page.request.post("/api/workspaces", {
    data: { name: `Empty window ${randomUUID()}` },
  });
  const { workspace } = await created.json();
  const channels = [];
  for (const name of ["empty-origin", "empty-neighbor"]) {
    const response = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name },
    });
    channels.push((await response.json()).channel);
  }
  const [origin, neighbor] = channels;
  await page.goto(`/app/${workspace.route_id}/${origin.route_id}`);
  await waitForAppReady(page);
  const postRequested = deferred(),
    postRelease = deferred(),
    postDelivered = deferred(),
    pageRequested = deferred(),
    pageRelease = deferred(),
    pageDelivered = deferred();
  await page.route(`**/api/channels/${origin.id}/messages*`, async (route) => {
    if (route.request().method() === "POST") {
      postRequested.resolve();
      await postRelease.promise;
      const response = await route.fetch();
      await route.fulfill({ response });
      postDelivered.resolve();
    } else {
      const response = await route.fetch();
      expect((await response.json()).messages).toEqual([]);
      pageRequested.resolve();
      await pageRelease.promise;
      await route.fulfill({ response });
      pageDelivered.resolve();
    }
  });
  try {
    await page.getByLabel("Message body", { exact: true }).fill("The first confirmed message");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await postRequested.promise;
    await page.locator(`#sidebar-channels-list a[href$="/${neighbor.route_id}"]`).click();
    await expect(page.getByRole("heading", { name: "#empty-neighbor", exact: true })).toBeVisible();
    await page.locator(`#sidebar-channels-list a[href$="/${origin.route_id}"]`).click();
    await pageRequested.promise;
    postRelease.resolve();
    await postDelivered.promise;
    await expect(page.locator(".message-row.is-pending")).toHaveCount(0);
    pageRelease.resolve();
    await pageDelivered.promise;
    await expect(
      page.locator(".message-row").getByText("The first confirmed message", { exact: true }),
    ).toBeVisible();
  } finally {
    postRelease.resolve();
    pageRelease.resolve();
  }
});

for (const kind of ["channel", "dm"] as const) {
  for (const change of ["edit", "delete", "bot deletion"] as const) {
    test(`${kind}: a background ${change} does not cancel returning a sent message to live chat`, async ({
      page,
    }) => {
      const { path, messages, workspace, bot } = await conversationFixture(page, kind, false, 10);
      await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
      await expectInTimeline(page, messages[10]);
      const received = deferred(),
        release = deferred(),
        delivered = deferred();
      await page.route(`**${path}/messages`, async (route) => {
        if (route.request().method() !== "POST") return route.continue();
        const response = await route.fetch();
        expect(response.status()).toBe(201);
        received.resolve();
        await release.promise;
        await route.fulfill({ response });
        delivered.resolve();
      });
      try {
        await page
          .getByLabel("Message body", { exact: true })
          .fill("Send after a background change");
        await page.getByRole("button", { name: "Send", exact: true }).click();
        await received.promise;
        const response =
          change === "edit"
            ? await page.request.patch(`/api/messages/${messages[98].id}`, {
                data: { body: "Background edit applied" },
              })
            : await page.request.delete(
                change === "delete" ? `/api/messages/${messages[98].id}` : `/api/bots/${bot.id}`,
              );
        expect(response.ok()).toBe(true);
        const events = await page.request.get(
          `/api/realtime/events?workspace_id=${workspace.id}&include_tail=true&limit=1`,
        );
        const { tail_cursor } = await events.json();
        await expect
          .poll(() =>
            page.evaluate((key) => localStorage.getItem(key), `clickclack:${workspace.id}:cursor`),
          )
          .toBe(tail_cursor);
        const changed = page.locator(`.message-row[data-message-id="${messages[98].id}"]`);
        if (change === "edit") await expect(changed).toContainText("Background edit applied");
        else if (change === "delete") await expect(changed).toHaveClass(/is-deleted/);
        release.resolve();
        await delivered.promise;
        await expect(
          page.locator(".message-row").getByText("Send after a background change", { exact: true }),
        ).toBeVisible();
        await expectInTimeline(page, messages[159]);
        await expect(
          page.locator(`.message-row[data-message-id="${messages[10].id}"]`),
        ).toHaveCount(0);
        if (change === "bot deletion")
          await expect(page.locator(".bot-chip--deleted").first()).toBeVisible();
      } finally {
        release.resolve();
      }
    });
  }

  for (const change of ["edit", "delete"] as const) {
    for (const action of ["live send", "explicit live jump"] as const) {
      test(`${kind}: ${action === "live send" ? "a" : "an"} ${action} preserves a pending ${change} refresh`, async ({
        page,
      }) => {
        const { path, messages, workspace } = await conversationFixture(page, kind, true, 2, 2);
        await settleScrollFrames(page);
        const target = messages[0];
        const row = page.locator(`.message-row[data-message-id="${target.id}"]`);
        await expectInTimeline(page, target);
        const entered = deferred(),
          release = deferred(),
          delivered = deferred();
        await page.route(`**/api/messages/${target.id}`, async (route) => {
          if (route.request().method() !== "GET") return route.continue();
          const response = await route.fetch();
          expect(response.ok()).toBe(true);
          entered.resolve();
          await release.promise;
          await route.fulfill({ response });
          delivered.resolve();
        });
        try {
          const changed =
            change === "edit"
              ? await page.request.patch(`/api/messages/${target.id}`, {
                  data: { body: "Live edge edit delivered" },
                })
              : await page.request.delete(`/api/messages/${target.id}`);
          expect(changed.ok()).toBe(true);
          const { event } = await changed.json();
          expect(event.cursor).toBeTruthy();
          await entered.promise;
          let revealed = messages[1];
          if (action === "live send") {
            const sent = page.waitForResponse(
              (response) =>
                response.request().method() === "POST" &&
                response.url().endsWith(`${path}/messages`),
            );
            await page
              .getByLabel("Message body", { exact: true })
              .fill("Send while an update is pending");
            await page.getByRole("button", { name: "Send", exact: true }).click();
            const receipt = await sent;
            expect(receipt.status()).toBe(201);
            revealed = (await receipt.json()).message;
            const sentRow = page.locator(`.message-row[data-message-id="${revealed.id}"]`);
            await expect(sentRow).not.toHaveClass(/is-(pending|failed)/);
          } else {
            await page.keyboard.press("Escape");
          }
          await expectInTimeline(page, revealed);
          await settleScrollFrames(page);
          release.resolve();
          await delivered.promise;
          await expect
            .poll(() =>
              page.evaluate(({ key, cursor }) => (localStorage.getItem(key) || "") >= cursor, {
                key: `clickclack:${workspace.id}:cursor`,
                cursor: event.cursor,
              }),
            )
            .toBe(true);
          if (change === "edit") await expect(row).toContainText("Live edge edit delivered");
          else await expect(row).toHaveClass(/is-deleted/);
          await expectInTimeline(page, revealed);
        } finally {
          release.resolve();
        }
      });
    }

    for (const loaded of [true, false]) {
      test(`${kind}: a latest snapshot preserves an intervening ${change} to a ${loaded ? "loaded" : "not-yet-loaded"} row`, async ({
        page,
      }) => {
        const { path, messages, workspace } = await conversationFixture(page, kind, true);
        await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
        await expectInTimeline(page, messages[10]);
        const target = messages[loaded ? 98 : 120];
        const entered = deferred(),
          release = deferred(),
          delivered = deferred();
        await page.route(`**${path}/messages?*`, async (route) => {
          const query = new URL(route.request().url()).searchParams;
          if (["around_seq", "before_seq", "after_seq"].some((key) => query.has(key)))
            return route.continue();
          const response = await route.fetch();
          const snapshot = await response.json();
          expect(snapshot.messages.some((message: Message) => message.id === target.id)).toBe(true);
          entered.resolve();
          await release.promise;
          await route.fulfill({ response });
          delivered.resolve();
        });
        try {
          await page
            .getByLabel("Message body", { exact: true })
            .fill("Send while latest is loading");
          await page.getByRole("button", { name: "Send", exact: true }).click();
          await entered.promise;
          const changed =
            change === "edit"
              ? await page.request.patch(`/api/messages/${target.id}`, {
                  data: { body: "Newer acknowledged edit" },
                })
              : await page.request.delete(`/api/messages/${target.id}`);
          expect(changed.ok()).toBe(true);
          const events = await page.request.get(
            `/api/realtime/events?workspace_id=${workspace.id}&include_tail=true&limit=1`,
          );
          const { tail_cursor } = await events.json();
          await expect
            .poll(() =>
              page.evaluate(
                (key) => localStorage.getItem(key),
                `clickclack:${workspace.id}:cursor`,
              ),
            )
            .toBe(tail_cursor);
          const row = page.locator(`[data-message-id="${target.id}"]`);
          if (loaded) {
            if (change === "edit") await expect(row).toContainText("Newer acknowledged edit");
            else await expect(row).toHaveClass(/is-deleted/);
          }
          release.resolve();
          await delivered.promise;
          await expect(page.locator(".messages")).not.toHaveClass(/is-revealing/);
          await settleScrollFrames(page);
          if (change === "edit") await expect(row).toContainText("Newer acknowledged edit");
          else await expect(row).toHaveClass(/is-deleted/);
          await expect(
            page.locator(".message-row").getByText("Send while latest is loading", { exact: true }),
          ).toBeVisible();
        } finally {
          release.resolve();
        }
      });
    }
  }

  for (const detour of ["another channel", "a pending thread"] as const) {
    test(`${kind}: revisiting a conversation after ${detour} cannot revive a retired row refresh`, async ({
      page,
    }) => {
      const { path, messages, workspace, conversation } = await conversationFixture(
        page,
        kind,
        true,
        2,
        2,
      );
      const neighborResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
        data: { name: "row-refresh-neighbor" },
      });
      expect(neighborResponse.ok()).toBe(true);
      const { channel } = await neighborResponse.json();
      let neighbor = page
        .locator(`a[href="/app/${workspace.route_id}/${channel.route_id}"]`)
        .first();
      await expect(neighbor).toBeVisible();
      const threadEntered = deferred(),
        threadRelease = deferred();
      if (detour === "a pending thread") {
        const rootResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
          data: { body: "Neighbor thread root" },
        });
        expect(rootResponse.ok()).toBe(true);
        const { message: root } = await rootResponse.json();
        const replyResponse = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
          data: { body: "Neighbor thread reply" },
        });
        expect(replyResponse.ok()).toBe(true);
        const threadResponse = await page.request.get(
          `/api/messages/${root.id}/thread?latest=true&limit=1`,
        );
        expect(threadResponse.ok()).toBe(true);
        const { root: routedRoot } = await threadResponse.json();
        const linkResponse = await page.request.post(`${path}/messages`, {
          data: {
            body: `[Open pending neighbor thread](/app/${workspace.route_id}/${routedRoot.route_id})`,
          },
        });
        expect(linkResponse.ok()).toBe(true);
        neighbor = page.getByRole("link", { name: "Open pending neighbor thread", exact: true });
        await expect(neighbor).toBeVisible();
        await page.route(`**/api/messages/${root.id}/thread?*`, async (route) => {
          const response = await route.fetch();
          expect(response.ok()).toBe(true);
          threadEntered.resolve();
          await threadRelease.promise;
          await route.fulfill({ response });
        });
      }
      const target = messages[0];
      const row = page.locator(`.message-row[data-message-id="${target.id}"]`);
      const entered = deferred(),
        release = deferred(),
        delivered = deferred(),
        releaseFollowups = deferred();
      let reads = 0;
      await page.route(`**/api/messages/${target.id}`, async (route) => {
        if (route.request().method() !== "GET") return route.continue();
        if (++reads > 1) {
          await releaseFollowups.promise;
          return route.continue();
        }
        const response = await route.fetch();
        expect(response.ok()).toBe(true);
        entered.resolve();
        await release.promise;
        await route.fulfill({ response });
        delivered.resolve();
      });
      try {
        const first = await page.request.patch(`/api/messages/${target.id}`, {
          data: { body: "Earlier edit before leaving" },
        });
        expect(first.ok()).toBe(true);
        const { event } = await first.json();
        await entered.promise;
        await neighbor.click();
        if (detour === "a pending thread") await threadEntered.promise;
        await expect(
          page.getByRole("heading", { name: "#row-refresh-neighbor", exact: true }),
        ).toBeVisible();
        const newer = await page.request.patch(`/api/messages/${target.id}`, {
          data: { body: "Newer edit after revisiting" },
        });
        expect(newer.ok()).toBe(true);
        await page
          .locator(`a[href="/app/${workspace.route_id}/${conversation.route_id}"]`)
          .first()
          .click();
        await expect(row).toContainText("Newer edit after revisiting");
        release.resolve();
        await delivered.promise;
        await expect
          .poll(() =>
            page.evaluate(({ key, cursor }) => (localStorage.getItem(key) || "") >= cursor, {
              key: `clickclack:${workspace.id}:cursor`,
              cursor: event.cursor,
            }),
          )
          .toBe(true);
        await expect(row).toContainText("Newer edit after revisiting");
        await expectInTimeline(page, target);
      } finally {
        release.resolve();
        releaseFollowups.resolve();
        threadRelease.resolve();
      }
    });
  }

  test(`${kind}: a stale row refresh preserves an acknowledged local edit`, async ({ page }) => {
    const { messages } = await conversationFixture(page, kind, true);
    await page.keyboard.press("Escape");
    const target = messages[159];
    const row = page.locator(`[data-message-id="${target.id}"]`);
    const entered = deferred(),
      release = deferred(),
      delivered = deferred(),
      releaseFollowups = deferred();
    let reads = 0;
    await page.route(`**/api/messages/${target.id}`, async (route) => {
      if (route.request().method() !== "GET") return route.continue();
      if (++reads > 1) {
        await releaseFollowups.promise;
        return route.continue();
      }
      const response = await route.fetch();
      entered.resolve();
      await release.promise;
      await route.fulfill({ response });
      delivered.resolve();
    });
    try {
      const remote = await page.request.patch(`/api/messages/${target.id}`, {
        data: { body: "Earlier remote edit" },
      });
      expect(remote.ok()).toBe(true);
      await entered.promise;
      await row.hover();
      await row.getByRole("button", { name: "More actions" }).click();
      await row.getByRole("menuitem", { name: "Edit message" }).click();
      await row.getByLabel("Edit message", { exact: true }).fill("Acknowledged local edit");
      await row.getByRole("button", { name: "Save", exact: true }).click();
      await expect(row.locator(".markdown")).toHaveText("Acknowledged local edit");
      release.resolve();
      await delivered.promise;
      await settleScrollFrames(page);
      await expect(row.locator(".markdown")).toHaveText("Acknowledged local edit");
    } finally {
      release.resolve();
      releaseFollowups.resolve();
    }
  });

  test(`${kind}: overlapping sent receipts survive an older latest snapshot`, async ({ page }) => {
    const { path, messages } = await conversationFixture(page, kind, true);
    await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
    await expectInTimeline(page, messages[10]);
    const firstRequested = deferred(),
      firstReceipt = deferred(),
      secondRequested = deferred(),
      secondSend = deferred(),
      latestRequested = deferred(),
      latestRelease = deferred(),
      latestDelivered = deferred();
    let posts = 0,
      heldLatest = false;
    await page.route(`**${path}/messages*`, async (route) => {
      const request = route.request(),
        query = new URL(request.url()).searchParams;
      if (request.method() === "POST") {
        const first = ++posts === 1;
        if (!first) {
          secondRequested.resolve();
          await secondSend.promise;
        }
        const response = await route.fetch();
        if (first) {
          firstRequested.resolve();
          await firstReceipt.promise;
        }
        await route.fulfill({ response });
      } else if (
        !heldLatest &&
        !["around_seq", "before_seq", "after_seq"].some((key) => query.has(key))
      ) {
        heldLatest = true;
        const response = await route.fetch();
        latestRequested.resolve();
        await latestRelease.promise;
        await route.fulfill({ response });
        latestDelivered.resolve();
      } else await route.continue();
    });
    try {
      const input = page.getByLabel("Message body", { exact: true });
      await input.fill("First overlapping send");
      await page.getByRole("button", { name: "Send", exact: true }).click();
      await firstRequested.promise;
      await input.fill("Second overlapping send");
      await page.getByRole("button", { name: "Send", exact: true }).click();
      await secondRequested.promise;
      firstReceipt.resolve();
      await latestRequested.promise;
      secondSend.resolve();
      await expect(
        page.locator(".message-row").filter({ hasText: "Second overlapping send" }),
      ).not.toHaveClass(/is-pending/);
      latestRelease.resolve();
      await latestDelivered.promise;
      await page.evaluate(
        () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          ),
      );
      await expect(
        page.locator(".message-row").getByText("First overlapping send", { exact: true }),
      ).toHaveCount(1);
      await expect(
        page.locator(".message-row").getByText("Second overlapping send", { exact: true }),
      ).toHaveCount(1);
      expect(posts).toBe(2);
    } finally {
      firstReceipt.resolve();
      secondSend.resolve();
      latestRelease.resolve();
    }
  });

  test(`${kind}: a delayed sent receipt preserves forward pagination through selected history`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 650 });
    const { path, messages } = await conversationFixture(page, kind, true);
    await page.locator(".search-result").filter({ hasText: "windowtarget Bravo" }).click();
    await expectInTimeline(page, messages[130]);
    const entered = deferred(),
      release = deferred(),
      delivered = deferred();
    await page.route(`**${path}/messages`, async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      const response = await route.fetch();
      entered.resolve();
      await release.promise;
      await route.fulfill({ response });
      delivered.resolve();
    });
    try {
      await page
        .getByLabel("Message body", { exact: true })
        .fill("Sent while selecting older history");
      await page.getByRole("button", { name: "Send", exact: true }).click();
      await entered.promise;
      await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
      await expectInTimeline(page, messages[10]);
      release.resolve();
      await delivered.promise;
      await page.evaluate(
        () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          ),
      );
      await expectInTimeline(page, messages[10]);
      const nextPage = page.waitForRequest(
        (request) =>
          request.method() === "GET" && request.url().includes(`${path}/messages?after_seq=`),
      );
      await page.locator(".messages-scroll").focus();
      await page.keyboard.press("End");
      expect(new URL((await nextPage).url()).searchParams.get("after_seq")).toBe("100");
      await expect(
        page.locator(`.message-row[data-message-id="${messages[100].id}"]`),
      ).toBeAttached();
    } finally {
      release.resolve();
    }
  });

  test(`${kind}: a confirmed send survives a failed latest-window refresh`, async ({
    page,
  }, testInfo) => {
    const { path, messages } = await conversationFixture(page, kind, true);
    await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
    await expectInTimeline(page, messages[10]);
    const failed = deferred();
    let posts = 0;
    await page.route(`**${path}/messages*`, async (route) => {
      const request = route.request();
      if (request.method() === "POST") posts++;
      const query = new URL(request.url()).searchParams;
      if (
        request.method() === "GET" &&
        !["around_seq", "before_seq", "after_seq"].some((key) => query.has(key))
      ) {
        await route.fulfill({ status: 503, json: { error: "Latest messages unavailable" } });
        failed.resolve();
      } else await route.continue();
    });
    const sent = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" && response.url().endsWith(`${path}/messages`),
    );
    const body = "Confirmed message from history";
    await page.getByLabel("Message body", { exact: true }).fill(body);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const receipt = await sent;
    expect(receipt.status()).toBe(201);
    const { message } = await receipt.json();
    await failed.promise;
    await expect(page.locator(".composer-notice__text")).toBeVisible();
    const saved = await page.request.get(`/api/messages/${message.id}`);
    expect((await saved.json()).message.body).toBe(body);
    await expect(page.getByText("Latest messages unavailable", { exact: true })).toBeVisible();
    await expect(page.locator(".message-row.is-failed")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Retry", exact: true })).toHaveCount(0);
    await expectInTimeline(page, message);
    await page.screenshot({ path: testInfo.outputPath("confirmed-send-refresh.png") });
    await expect(page.getByLabel("Message body", { exact: true })).toHaveValue("");
    await page.unroute(`**${path}/messages*`);
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
    await expectInTimeline(page, message);
    expect(posts).toBe(1);
    const latest = await page.request.get(`${path}/messages?mode=latest`);
    expect((await latest.json()).messages.filter((row: Message) => row.body === body)).toHaveLength(
      1,
    );
  });

  for (const completion of ["receipt", "refresh"] as const) {
    test(`${kind}: a confirmed send's delayed ${completion} preserves a newer history selection`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: 1440, height: 650 });
      const { path, messages } = await conversationFixture(page, kind, true);
      await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
      await expectInTimeline(page, messages[10]);
      const entered = deferred(),
        release = deferred(),
        delivered = deferred();
      let held = false;
      await page.route(`**${path}/messages*`, async (route) => {
        const request = route.request();
        const query = new URL(request.url()).searchParams;
        const matches =
          completion === "receipt"
            ? request.method() === "POST"
            : request.method() === "GET" &&
              !["around_seq", "before_seq", "after_seq"].some((key) => query.has(key));
        if (held || !matches) return route.continue();
        held = true;
        const response = await route.fetch();
        entered.resolve();
        await release.promise;
        await route.fulfill({ response });
        delivered.resolve();
      });
      try {
        await page
          .getByLabel("Message body", { exact: true })
          .fill("Send before selecting history");
        await page.getByRole("button", { name: "Send", exact: true }).click();
        await entered.promise;
        await page.locator(".search-result").filter({ hasText: "windowtarget Bravo" }).click();
        await expectInTimeline(page, messages[130]);
        release.resolve();
        await delivered.promise;
        await page.evaluate(
          () =>
            new Promise<void>((resolve) =>
              requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
            ),
        );
        await expectInTimeline(page, messages[130]);
        await expect(
          page.locator(".search-result").filter({ hasText: "windowtarget Bravo" }),
        ).toHaveClass(/active/);
      } finally {
        release.resolve();
      }
    });
  }

  for (const failure of ["send", "attachment"] as const) {
    test(`${kind}: a delayed ${failure} failure preserves a newer history selection`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: 1440, height: 650 });
      const { path, messages } = await conversationFixture(page, kind, true);
      await page.locator(".search-result").filter({ hasText: "windowtarget Bravo" }).click();
      await expectInTimeline(page, messages[130]);
      if (failure === "attachment") {
        await page.getByLabel("Upload file", { exact: true }).setInputFiles({
          name: "history-attachment.txt",
          mimeType: "text/plain",
          buffer: Buffer.from("Attachment sent before selecting history"),
        });
        await expect(page.locator(".attachment-name")).toContainText("history-attachment.txt");
      }
      const entered = deferred(),
        release = deferred(),
        delivered = deferred();
      await page.route(
        failure === "send" ? `**${path}/messages` : "**/api/messages/*/attachments",
        async (route) => {
          if (route.request().method() !== "POST") return route.continue();
          entered.resolve();
          await release.promise;
          await route.fulfill({ status: 503, json: { error: "Submission unavailable" } });
          delivered.resolve();
        },
      );
      try {
        await page.getByLabel("Message body", { exact: true }).fill("A delayed failed draft");
        await page.getByRole("button", { name: "Send", exact: true }).click();
        await entered.promise;
        await page.locator(".search-result").filter({ hasText: "windowtarget Alpha" }).click();
        await expectInTimeline(page, messages[10]);
        release.resolve();
        await delivered.promise;
        await expect(page.locator(".composer-notice__text")).toContainText(
          failure === "send" ? "failed to send" : "attachment failed",
        );
        await settleScrollFrames(page);
        await expectInTimeline(page, messages[10]);
        await expect(page.locator(".message-row.is-failed")).toHaveCount(1);
      } finally {
        release.resolve();
      }
    });
  }

  test(`${kind}: latest selected during an initial load settles the selected conversation`, async ({
    page,
  }) => {
    test.setTimeout(90_000);
    const { path, messages, workspace, conversation } = await conversationFixture(page, kind);
    await page.keyboard.press("Escape");
    const response = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "empty-neighbor" },
    });
    expect(response.ok()).toBe(true);
    const { channel } = await response.json();
    await page.locator(`a[href="/app/${workspace.route_id}/${channel.route_id}"]`).first().click();
    await expect(page.getByRole("heading", { name: "#empty-neighbor", exact: true })).toBeVisible();
    const entered = deferred(),
      release = deferred(),
      delivered = deferred();
    let held = false;
    await page.route(`**${path}/messages?*`, async (route) => {
      if (held) return route.continue();
      held = true;
      const response = await route.fetch();
      entered.resolve();
      await release.promise;
      await route.fulfill({ response });
      delivered.resolve();
    });
    try {
      await page
        .locator(`a[href="/app/${workspace.route_id}/${conversation.route_id}"]`)
        .first()
        .click();
      await entered.promise;
      await page.keyboard.press("Escape");
      await expectInTimeline(page, messages[159]);
      await expect(page.locator(".messages")).not.toHaveClass(/is-revealing/);
      release.resolve();
      await delivered.promise;
      await expectInTimeline(page, messages[159]);
    } finally {
      release.resolve();
    }
  });

  for (const [choice, singleAuthor] of [
    ["result", false],
    ["latest", false],
    ["around", false],
    ["result", true],
  ] as const) {
    test(`${kind}${singleAuthor ? " single-author" : ""}: a delayed ${choice === "around" ? "latest" : "around"} response preserves the newer ${choice} selection`, async ({
      page,
    }) => {
      test.setTimeout(90_000);
      const { path, messages } = await conversationFixture(page, kind, singleAuthor);
      const alpha = page.locator(".search-result").filter({ hasText: "windowtarget Alpha" });
      const bravo = page.locator(".search-result").filter({ hasText: "windowtarget Bravo" });
      if (choice === "around") {
        await alpha.click();
        await expectInTimeline(page, messages[10]);
      }
      const entered = deferred(),
        release = deferred(),
        delivered = deferred();
      let held = false;
      await page.route(`**${path}/messages?*`, async (route) => {
        const query = new URL(route.request().url()).searchParams;
        const matches =
          choice === "around"
            ? query.get("limit") === "100" &&
              !query.has("around_seq") &&
              !query.has("before_seq") &&
              !query.has("after_seq")
            : query.get("around_seq") === "11";
        if (held || !matches) return route.continue();
        held = true;
        const response = await route.fetch();
        entered.resolve();
        await release.promise;
        await route.fulfill({ response });
        delivered.resolve();
      });
      try {
        if (choice === "around") {
          await page.keyboard.press("Escape");
          await page.keyboard.press("Escape");
        } else await alpha.click();
        await entered.promise;
        if (choice === "latest") {
          await page.keyboard.press("Escape");
          await page.keyboard.press("Escape");
        } else if (choice === "around") {
          await page.getByLabel("Search messages").fill("windowtarget");
          await page.getByRole("button", { name: "Search", exact: true }).click();
          await alpha.click();
        } else await bravo.press("Enter");
        const selected = messages[choice === "result" ? 130 : choice === "latest" ? 159 : 10];
        await expectInTimeline(page, selected);
        release.resolve();
        await delivered.promise;
        // Give the released fetch and any queued scroll command time to commit.
        await page.evaluate(
          () =>
            new Promise<void>((resolve) =>
              requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
            ),
        );
        await expectInTimeline(page, selected);
        if (choice !== "latest")
          await expect(choice === "result" ? bravo : alpha).toHaveClass(/active/);
        await expect(
          page.locator(
            `.message-row[data-message-id="${messages[choice === "around" ? 159 : 10].id}"]`,
          ),
        ).toHaveCount(0);
      } finally {
        release.resolve();
      }
    });
  }
}
test("a delayed older page preserves the selected window and one attached send", async ({
  page,
}) => {
  const { path, messages } = await conversationFixture(page, "channel", true);
  const pageErrors: Error[] = [];
  page.on("pageerror", (error) => pageErrors.push(error));
  await page.getByLabel("Upload file", { exact: true }).setInputFiles({
    name: "receipt.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("Synthetic attachment receipt"),
  });
  await expect(page.locator(".attachment-name")).toContainText("receipt.txt");
  const olderRequested = deferred(),
    olderRelease = deferred(),
    olderDelivered = deferred(),
    attachmentRequested = deferred(),
    attachmentRelease = deferred(),
    refreshRelease = deferred();
  let sentID = "";
  await page.route(`**${path}/messages?*`, async (route) => {
    const query = new URL(route.request().url()).searchParams;
    if (query.get("before_seq") === "61") {
      const response = await route.fetch();
      olderRequested.resolve();
      await olderRelease.promise;
      await route.fulfill({ response });
      olderDelivered.resolve();
    } else await route.continue();
  });
  await page.route("**/api/messages/*", async (route) => {
    if (route.request().method() !== "GET" || !route.request().url().endsWith(`/${sentID}`))
      return route.continue();
    // Deliver the attachment refresh after the older page, keeping this race ordered.
    const response = await route.fetch();
    await refreshRelease.promise;
    await route.fulfill({ response });
  });
  await page.route("**/api/messages/*/attachments", async (route) => {
    attachmentRequested.resolve();
    await attachmentRelease.promise;
    const response = await route.fetch();
    expect(response.ok()).toBe(true);
    await route.fulfill({ response });
  });
  try {
    await page.locator(".messages-scroll").focus();
    await page.keyboard.press("Home");
    await olderRequested.promise;
    const sentResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        new URL(response.url()).pathname === `${path}/messages`,
    );
    await page.getByLabel("Message body", { exact: true }).fill("One confirmed attached send");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const { message } = await (await sentResponse).json();
    sentID = message.id;
    await attachmentRequested.promise;
    const bravo = page.locator(".search-result").filter({ hasText: "windowtarget Bravo" });
    await bravo.click();
    await expectInTimeline(page, messages[130]);
    const row = page.locator(`.message-row[data-message-id="${message.id}"]`);
    await expect(row).toHaveCount(1);
    await expect(row).toHaveClass(/is-pending/);
    attachmentRelease.resolve();
    await expect(row).not.toHaveClass(/is-(pending|failed)/);
    await expect(row.getByText("receipt.txt", { exact: true })).toBeVisible();
    olderRelease.resolve();
    await olderDelivered.promise;
    await page.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    expect(pageErrors).toEqual([]);
    await expect(row).toHaveCount(1);
    await expectInTimeline(page, messages[130]);
    await expect(bravo).toHaveClass(/active/);
    const nextOlder = page.waitForRequest(
      (request) =>
        request.method() === "GET" && request.url().includes(`${path}/messages?before_seq=`),
    );
    await page.locator(".messages-scroll").focus();
    await page.keyboard.press("Home");
    expect(new URL((await nextOlder).url()).searchParams.get("before_seq")).toBe("62");
    await expect(page.locator(`.message-row[data-message-id="${messages[60].id}"]`)).toBeAttached();
    expect(pageErrors).toEqual([]);
  } finally {
    olderRelease.resolve();
    attachmentRelease.resolve();
    refreshRelease.resolve();
  }
});
