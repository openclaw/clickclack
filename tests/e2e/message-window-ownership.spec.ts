import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Message } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { deferred } from "./thread-fixture";

async function conversationFixture(page: Page, kind: "channel" | "dm", singleAuthor = false) {
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
  for (let seq = 1; seq <= 160; seq++) {
    const body =
      seq === 11 ? "windowtarget Alpha" : seq === 131 ? "windowtarget Bravo" : `Row ${seq}`;
    const response = await page.request.post(`${path}/messages`, {
      headers: !singleAuthor && seq % 2 === 0 ? { "X-ClickClack-User": bot.id } : {},
      data: { body },
    });
    expect(response.ok()).toBe(true);
    messages.push((await response.json()).message);
  }
  expect((await page.request.post(`${path}/read`, { data: { seq: 160 } })).ok()).toBe(true);
  await page.goto(`/app/${workspace.route_id}/${conversation.route_id}`);
  await waitForAppReady(page);
  await expect(page.locator(`.message-row[data-message-id="${messages[159].id}"]`)).toBeVisible();
  await page.getByLabel("Search messages").fill("windowtarget");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.locator(".search-result")).toHaveCount(2);
  return { path, messages, workspace, conversation };
}

async function expectInTimeline(page: Page, message: Message) {
  await expect(page.locator(`.message-row[data-message-id="${message.id}"]`)).toBeInViewport({
    ratio: 0.5,
  });
}

for (const kind of ["channel", "dm"] as const) {
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
