import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { deflateSync } from "node:zlib";
import { waitForAppReady } from "./app-ready";
import { settleScrollFrames } from "./message-frames";

// Adding a reaction to a message grows its row. The chip the user just tapped
// must end up in view, measured on the chip itself, and a scroll the user
// makes while the request is pending must stand.

type Fixture = { channelID: string; route: string };

async function setup(page: Page, name: string): Promise<Fixture> {
  const suffix = randomUUID().slice(0, 8);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `${name} ${suffix}` },
  });
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `${name.toLowerCase()}-${suffix}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  return { channelID: channel.id, route: `/app/${workspace.route_id}/${channel.route_id}` };
}

async function seed(page: Page, channelID: string, count: number): Promise<string> {
  let lastID = "";
  for (let i = 0; i < count; i++) {
    const response = await page.request.post(`/api/channels/${channelID}/messages`, {
      data: { body: `Message ${i}: the quick brown fox jumps over the lazy dog and keeps going.` },
    });
    lastID = ((await response.json()) as { message: { id: string } }).message.id;
  }
  return lastID;
}

async function openSettled(page: Page, route: string) {
  await page.goto(route);
  await waitForAppReady(page);
  await expect(page.locator(".messages.is-revealing")).toHaveCount(0);
  await settleScrollFrames(page);
}

function scrollBy(page: Page, delta: number) {
  return page.locator(".messages-scroll").evaluate((el, d) => {
    el.scrollTop += d;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  }, delta);
}

function pinToBottom(page: Page) {
  return page.locator(".messages-scroll").evaluate((el) => {
    el.scrollTop = el.scrollHeight;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
}

function barPlacement(page: Page, messageID: string) {
  return page.evaluate((id) => {
    const view = document.querySelector(".messages-scroll")!.getBoundingClientRect();
    const bar = document
      .querySelector(`[data-message-id="${id}"] .reactions-bar`)!
      .getBoundingClientRect();
    return { barTop: bar.top, barBottom: bar.bottom, viewTop: view.top, viewBottom: view.bottom };
  }, messageID);
}

async function reactFromToolbar(page: Page, messageID: string) {
  const row = page.locator(`[data-message-id="${messageID}"]`).first();
  const box = (await row.boundingBox())!;
  await page.mouse.move(box.x + 120, box.y + 12);
  await row.getByRole("button", { name: "React with 👍" }).click();
  await expect(row.locator(".reactions-bar button").first()).toBeVisible();
}

// A tall solid PNG, so several attachments stack well past one phone screen.
function tallPNG(width: number, height: number): Buffer {
  const crcTable = new Int32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    crcTable[n] = c;
  }
  const crc = (buffer: Buffer) => {
    let c = -1;
    for (const byte of buffer) c = crcTable[(c ^ byte) & 0xff] ^ (c >>> 8);
    return (c ^ -1) >>> 0;
  };
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4);
    length.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([length, body, sum]);
  };
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 2;
  const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(width * 3, 0x66)]);
  const raw = Buffer.concat(Array.from({ length: height }, () => row));
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

test("a reaction added to the last message stays in view", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fixture = await setup(page, "Reveal");
  const lastID = await seed(page, fixture.channelID, 14);
  await openSettled(page, fixture.route);
  // Pin to the bottom, then leave the list a little short of it, the way a
  // trackpad or a finger usually does.
  await pinToBottom(page);
  await settleScrollFrames(page);
  await scrollBy(page, -24);
  await settleScrollFrames(page);

  await reactFromToolbar(page, lastID);
  await settleScrollFrames(page);
  if (process.env.PROOF_OUT) {
    await page.screenshot({
      path: `${process.env.PROOF_OUT}/${process.env.PROOF_TAG}-reveal.png`,
      clip: { x: 0, y: 560, width: 390, height: 284 },
    });
  }
  const placement = await barPlacement(page, lastID);
  expect(placement.barBottom).toBeLessThanOrEqual(placement.viewBottom);
  expect(placement.barTop).toBeGreaterThanOrEqual(placement.viewTop);
});

test("revealing a reaction on a message with tall attachments keeps the chip in view", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fixture = await setup(page, "Tall");
  await seed(page, fixture.channelID, 3);
  await openSettled(page, fixture.route);
  test.slow();
  const image = tallPNG(320, 520);
  for (const name of ["a", "b", "c"]) {
    await page
      .getByLabel("Upload file")
      .setInputFiles({ name: `tall-${name}.png`, mimeType: "image/png", buffer: image });
    await expect(page.getByText(`tall-${name}.png`)).toBeVisible();
  }
  const text = `stacked images ${randomUUID().slice(0, 6)}`;
  await page.getByLabel("Message body").fill(text);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)").filter({ hasText: text });
  await expect(row.getByRole("button", { name: "Open image tall-c.png" })).toBeVisible();
  const messageID = (await row.getAttribute("data-message-id"))!;
  await settleScrollFrames(page);

  // Put the message text near the top of the viewport: the chip will render
  // right under it, in view, with the attachments stacked far below.
  await page.evaluate((id) => {
    const scroller = document.querySelector(".messages-scroll")!;
    const el = document.querySelector(`[data-message-id="${id}"]`)!;
    scroller.scrollTop += el.getBoundingClientRect().top - scroller.getBoundingClientRect().top - 8;
    scroller.dispatchEvent(new Event("scroll", { bubbles: true }));
  }, messageID);
  await settleScrollFrames(page);
  const before = await page.locator(".messages-scroll").evaluate((el) => el.scrollTop);

  await reactFromToolbar(page, messageID);
  await settleScrollFrames(page);
  const placement = await barPlacement(page, messageID);
  expect(placement.barTop).toBeGreaterThanOrEqual(placement.viewTop);
  expect(placement.barBottom).toBeLessThanOrEqual(placement.viewBottom);
  const after = await page.locator(".messages-scroll").evaluate((el) => el.scrollTop);
  expect(Math.abs(after - before)).toBeLessThan(4);
});

test("a scroll made while the reaction request is pending stands", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fixture = await setup(page, "Pending");
  const lastID = await seed(page, fixture.channelID, 14);
  await openSettled(page, fixture.route);
  await pinToBottom(page);
  await settleScrollFrames(page);

  let release: (() => void) | undefined;
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(`**/api/messages/${lastID}/reactions`, async (route) => {
    await held;
    await route.continue();
  });

  await reactFromToolbar(page, lastID);
  await settleScrollFrames(page);
  await scrollBy(page, -300);
  await settleScrollFrames(page);
  const scrolledUp = await page.locator(".messages-scroll").evaluate((el) => el.scrollTop);

  release?.();
  await expect
    .poll(async () => {
      const response = await page.request.get(`/api/messages/${lastID}`);
      const { message } = (await response.json()) as { message: { reactions?: unknown[] } };
      return message.reactions?.length ?? 0;
    })
    .toBeGreaterThan(0);
  await page.waitForTimeout(400);
  await settleScrollFrames(page);
  const settled = await page.locator(".messages-scroll").evaluate((el) => el.scrollTop);
  expect(Math.abs(settled - scrolledUp)).toBeLessThan(4);
});

test("removing the last reaction does not move the list", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const fixture = await setup(page, "Remove");
  await seed(page, fixture.channelID, 3);
  await openSettled(page, fixture.route);
  test.slow();
  const image = tallPNG(320, 520);
  for (const name of ["a", "b"]) {
    await page
      .getByLabel("Upload file")
      .setInputFiles({ name: `tall-${name}.png`, mimeType: "image/png", buffer: image });
    await expect(page.getByText(`tall-${name}.png`)).toBeVisible();
  }
  const text = `remove me ${randomUUID().slice(0, 6)}`;
  await page.getByLabel("Message body").fill(text);
  await page.getByRole("button", { name: "Send" }).click();
  const row = page.locator(".message-row:not(.is-pending)").filter({ hasText: text });
  await expect(row.getByRole("button", { name: "Open image tall-b.png" })).toBeVisible();
  const messageID = (await row.getAttribute("data-message-id"))!;
  await settleScrollFrames(page);
  await reactFromToolbar(page, messageID);
  const chip = row.locator(".reactions-bar button").first();
  await page.evaluate((id) => {
    const scroller = document.querySelector(".messages-scroll")!;
    const el = document.querySelector(`[data-message-id="${id}"]`)!;
    scroller.scrollTop += el.getBoundingClientRect().top - scroller.getBoundingClientRect().top - 8;
    scroller.dispatchEvent(new Event("scroll", { bubbles: true }));
  }, messageID);
  await settleScrollFrames(page);
  // The message text is what the user is reading; it must stay put. The list
  // may re-measure the shrinking row by a few pixels, so the tolerance is
  // well under the chip's height and far under a scroll toward the images.
  const rowTop = () => row.evaluate((el) => el.getBoundingClientRect().top);
  const before = await rowTop();
  await chip.click();
  await expect(row.locator(".reactions-bar")).toHaveCount(0);
  await settleScrollFrames(page);
  // A scroll toward the images would move it by hundreds of pixels.
  expect(Math.abs((await rowTop()) - before)).toBeLessThan(60);
});
