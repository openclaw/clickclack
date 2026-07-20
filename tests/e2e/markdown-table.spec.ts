import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

test("wide Markdown tables remain reachable by keyboard in ClickClack", async ({ page }) => {
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

  await page
    .getByLabel("Message body")
    .fill(
      [
        "| Deployment target with intentionally wide content | Current status | Owner count |",
        "| :--- | :---: | ---: |",
        "| production-us-west-service-cluster | Healthy and serving traffic | 128 active owners |",
      ].join("\n"),
    );
  await page.getByRole("button", { name: "Send" }).click();

  const row = page.locator(".message-row:not(.is-pending)", {
    has: page.getByText("production-us-west-service-cluster", { exact: true }),
  });
  const scroller = row.getByRole("region", { name: "Scrollable Markdown table" });
  await expect(scroller).toBeVisible();
  await expect
    .poll(() => scroller.evaluate((node) => node.scrollWidth > node.clientWidth))
    .toBe(true);

  await scroller.focus();
  await expect(scroller).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => scroller.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0);

  const alignments = await scroller
    .locator("tbody td")
    .evaluateAll((cells) => cells.map((cell) => getComputedStyle(cell).textAlign));
  expect(alignments).toEqual(["-webkit-left", "-webkit-center", "-webkit-right"]);
});
