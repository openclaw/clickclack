import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

test("Markdown tables stay contained and expose scrolling only when needed", async ({ page }) => {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Markdown Table ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `table-proof-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { route_id: string; name: string };
  };

  await page.setViewportSize({ width: 480, height: 720 });
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  await expect(page.getByRole("heading", { name: `#${channel.name}` })).toBeVisible();

  const sendMessage = async (body: string) => {
    await page.getByLabel("Message body").fill(body);
    await page.getByRole("button", { name: "Send" }).click();
  };

  const gfmMarker = `production-us-west-service-cluster-${suffix}`;
  await sendMessage(
    [
      "| Deployment target with intentionally wide content | Current status | Owner count |",
      "| :--- | :---: | ---: |",
      `| ${gfmMarker} | Healthy and serving traffic | 128 active owners |`,
    ].join("\n"),
  );

  const row = page.locator(".message-row:not(.is-pending)", {
    has: page.getByText(gfmMarker, { exact: true }),
  });
  const scroller = row.locator(".markdown-table-scroll");
  await expect(scroller).toBeVisible();
  await expect
    .poll(() => scroller.evaluate((node) => node.scrollWidth > node.clientWidth))
    .toBe(true);
  await expect(scroller).toHaveAttribute("role", "group");
  await expect(scroller).toHaveAttribute("aria-label", "Scrollable table");
  await expect(scroller).toHaveAttribute("tabindex", "0");

  await scroller.focus();
  await expect(scroller).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => scroller.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0);

  const alignments = await scroller
    .locator("tbody td")
    .evaluateAll((cells) => cells.map((cell) => cell.getAttribute("align")));
  expect(alignments).toEqual(["left", "center", "right"]);

  const threadPane = page.getByLabel("Thread pane", { exact: true });
  await expect(threadPane.getByRole("button", { name: "Close thread" })).toBeHidden();
  await scroller.locator("tbody td").first().click();
  await expect(threadPane.getByRole("button", { name: "Close thread" })).toBeHidden();

  await page.setViewportSize({ width: 1440, height: 900 });
  await expect
    .poll(() => scroller.evaluate((node) => node.hasAttribute("data-overflowing")))
    .toBe(false);
  await expect(scroller).not.toHaveAttribute("role", "group");
  await expect(scroller).not.toHaveAttribute("aria-label", "Scrollable table");
  await expect(scroller).not.toHaveAttribute("tabindex", "0");

  await page.setViewportSize({ width: 480, height: 720 });
  await expect
    .poll(() => scroller.evaluate((node) => node.hasAttribute("data-overflowing")))
    .toBe(true);
  await expect(scroller).toHaveAttribute("role", "group");
  await expect(scroller).toHaveAttribute("tabindex", "0");
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1,
      ),
    )
    .toBe(true);

  const rawMarker = `raw-html-wide-${suffix}-${"x".repeat(80)}`;
  await sendMessage(
    `<table><thead><tr><th>Raw HTML path</th><th>Status</th></tr></thead><tbody><tr><td>${rawMarker}</td><td>contained</td></tr></tbody></table>`,
  );
  const rawRow = page.locator(".message-row:not(.is-pending)", {
    has: page.getByText(rawMarker, { exact: true }),
  });
  const rawScroller = rawRow.locator(".markdown-table-scroll");
  await expect(rawScroller).toBeVisible();
  await expect
    .poll(() => rawScroller.evaluate((node) => node.scrollWidth > node.clientWidth))
    .toBe(true);
  await expect(rawScroller).toHaveAttribute("role", "group");
  await expect(rawScroller).toHaveAttribute("tabindex", "0");

  const adoptedMarker = `prewrapped-raw-${suffix}-${"z".repeat(80)}`;
  await sendMessage(
    `<div class="markdown-table-scroll"><table><tbody><tr><td>${adoptedMarker}</td><td>normalized</td></tr></tbody></table></div>`,
  );
  const adoptedRow = page.locator(".message-row:not(.is-pending)", {
    has: page.getByText(adoptedMarker, { exact: true }),
  });
  const adoptedScroller = adoptedRow.locator(".markdown-table-scroll");
  await expect(adoptedScroller).toHaveCount(1);
  await expect(adoptedScroller).toHaveAttribute("role", "group");
  await expect(adoptedScroller).toHaveAttribute("tabindex", "0");

  const narrowMarker = `narrow-${suffix}`;
  await sendMessage(`| Key | Value |\n| --- | --- |\n| ${narrowMarker} | Fits |`);
  const narrowRow = page.locator(".message-row:not(.is-pending)", {
    has: page.getByText(narrowMarker, { exact: true }),
  });
  const narrowScroller = narrowRow.locator(".markdown-table-scroll");
  await expect(narrowScroller).toBeVisible();
  await expect
    .poll(() => narrowScroller.evaluate((node) => node.scrollWidth > node.clientWidth))
    .toBe(false);
  await expect(narrowScroller).not.toHaveAttribute("data-overflowing", "");
  await expect(narrowScroller).not.toHaveAttribute("role", "group");
  await expect(narrowScroller).not.toHaveAttribute("aria-label", "Scrollable table");
  await expect(narrowScroller).not.toHaveAttribute("tabindex", "0");

  await row.hover();
  await row.getByRole("button", { name: "Open thread" }).click();
  await expect(threadPane.getByRole("button", { name: "Close thread" })).toBeVisible();
  const threadRootScroller = threadPane.locator(".thread-root .markdown-table-scroll");
  await expect(threadRootScroller).toBeVisible();

  const rootID = await row.getAttribute("data-message-id");
  expect(rootID).toBeTruthy();
  const replyMarker = `raw-thread-reply-${suffix}-${"y".repeat(48)}`;
  const replyResponse = await page.request.post(`/api/messages/${rootID}/thread/replies`, {
    data: {
      body: `<table><tbody><tr><td>${replyMarker}</td><td>reachable</td></tr></tbody></table>`,
    },
  });
  expect(replyResponse.ok()).toBe(true);
  const { message: reply } = (await replyResponse.json()) as { message: { id: string } };
  const replyScroller = threadPane.locator(
    `[data-message-id="${reply.id}"] .markdown-table-scroll`,
  );
  await expect(replyScroller).toBeVisible();
  await expect(replyScroller.getByText(replyMarker, { exact: true })).toBeVisible();
});
