import { expect, type Page, type Locator } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

export function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

export async function threadFixture(page: Page) {
  const suffix = randomUUID().slice(0, 8);
  const created = await page.request.post("/api/workspaces", {
    data: { name: `Thread lifecycle ${suffix}` },
  });
  expect(created.ok()).toBe(true);
  const { workspace } = await created.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `threads-${suffix}` },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = await channelResponse.json();
  const roots = [];
  for (const label of ["First", "Second"]) {
    const response = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: `${label} thread ${suffix}` },
    });
    expect(response.ok()).toBe(true);
    const { message } = await response.json();
    const reply = await page.request.post(`/api/messages/${message.id}/thread/replies`, {
      data: { body: `${label} existing reply` },
    });
    expect(reply.ok()).toBe(true);
    const route = await page.request.post(`/api/messages/${message.id}/route`);
    expect(route.ok()).toBe(true);
    const routed = await page.request.get(`/api/messages/${message.id}`);
    roots.push((await routed.json()).message);
  }
  await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
  await waitForAppReady(page);
  return { roots, channel, workspace };
}

export async function openThread(page: Page, id: string) {
  const row = page.locator(`.message-row[data-message-id="${id}"]`);
  await row.hover();
  await row.getByRole("button", { name: "Open thread", exact: true }).click();
}

export async function longThread(page: Page, count = 128) {
  const fixture = await threadFixture(page);
  const root = fixture.roots[0];
  const first = await page.request.get(`/api/messages/${root.id}/thread?limit=1`);
  const replies = (await first.json()).replies;
  for (let n = 2; n <= count; n++) {
    const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
      data: { body: `Window reply ${n}` },
    });
    expect(response.ok()).toBe(true);
    replies.push((await response.json()).message);
  }
  await openThread(page, root.id);
  await expect(page.locator(`.reply[data-message-id="${replies.at(-1).id}"]`)).toBeAttached();
  return { ...fixture, root, replies };
}

export async function scrollThreadTo(row: Locator, page: Page) {
  const viewport = page.getByRole("region", { name: "Thread messages", exact: true });
  await viewport.hover();
  // Real input cancels a queued history restore before selecting a reading anchor.
  await page.mouse.wheel(0, (await row.boundingBox())!.y - (await viewport.boundingBox())!.y);
  await expectInsideThread(row, page);
}

export async function expectInsideThread(row: Locator, page: Page) {
  await expect
    .poll(async () => {
      const bounds = await row.boundingBox();
      const viewport = await page
        .getByRole("region", { name: "Thread messages", exact: true })
        .boundingBox();
      return Boolean(
        bounds &&
        viewport &&
        bounds.y >= viewport.y - 2 &&
        bounds.y < viewport.y + viewport.height &&
        bounds.y + bounds.height > viewport.y,
      );
    })
    .toBe(true);
}
