import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Channel, Message, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";
import { settleScrollFrames } from "./message-frames";
// A tablet with a trackpad: the primary pointer is coarse and cannot hover, but a
// fine, hovering pointer is attached (any-hover: hover, any-pointer: fine). This
// is what iPadOS reports with a trackpad or mouse connected.
test.use({
  hasTouch: false,
  launchOptions: {
    args: [
      "--blink-settings=primaryPointerType=2,availablePointerTypes=6,primaryHoverType=1,availableHoverTypes=3",
    ],
  },
});
for (const surface of ["chat", "embed"] as const) {
  test(`trackpad on a touch-first device can open ${surface} message actions`, async ({ page }) => {
    const x = randomUUID().slice(0, 8);
    const ws = (await (
      await page.request.post("/api/workspaces", { data: { name: `Pad ${x}` } })
    ).json()) as { workspace: Workspace };
    const ch = (await (
      await page.request.post(`/api/workspaces/${ws.workspace.id}/channels`, {
        data: { name: `pad-${x}`, kind: "public" },
      })
    ).json()) as { channel: Channel };
    const bot = (await (
      await page.request.post(`/api/workspaces/${ws.workspace.id}/bots`, {
        data: {
          display_name: "Blackbird",
          handle: `blackbird-${x}`,
          token_name: "e2e",
          scopes: ["bot:write"],
        },
      })
    ).json()) as { bot_token: { token: string } };
    for (let i = 0; i < 8; i++) {
      await page.request.post(`/api/channels/${ch.channel.id}/messages`, {
        data: { body: `Earlier message ${i}` },
      });
    }
    const posted = await page.request.post(`/api/channels/${ch.channel.id}/messages`, {
      headers: { Authorization: `Bearer ${bot.bot_token.token}` },
      data: { body: `Trophy line ${x}` },
    });
    const msg = ((await posted.json()) as { message: Message }).message;
    const prefix = surface === "chat" ? "/app" : "/embed/channel";
    await page.goto(`${prefix}/${ws.workspace.route_id}/${ch.channel.route_id}`);
    if (surface === "chat") await waitForAppReady(page);
    else await expect(page.getByLabel("Message body")).toBeVisible();
    await expect(page.locator(".messages.is-revealing")).toHaveCount(0);
    await settleScrollFrames(page);
    const mq = await page.evaluate(() => ({
      hover: matchMedia("(hover: hover)").matches,
      anyHover: matchMedia("(any-hover: hover)").matches,
      coarse: matchMedia("(pointer: coarse)").matches,
      anyFine: matchMedia("(any-pointer: fine)").matches,
    }));
    expect(mq).toEqual({ hover: false, anyHover: true, coarse: true, anyFine: true });
    const row = page.locator(`[data-message-id="${msg.id}"]`).first();
    const box = (await row.boundingBox())!;
    await page.mouse.move(box.x + 200, box.y + box.height / 2);
    const add = row.getByRole("button", { name: "Add reaction" });
    if (process.env.PROOF_OUT)
      await page.screenshot({
        path: `${process.env.PROOF_OUT}/${process.env.PROOF_TAG}-hover.png`,
        clip: { x: 0, y: Math.max(0, box.y - 70), width: 1280, height: 150 },
      });
    await expect(add).toBeVisible();
    const size = await add.boundingBox();
    expect(size!.width).toBeGreaterThan(12);
    await add.click();
    await row.locator("button.emoji-option").first().click();
    await expect(row.locator(".reactions-bar button").first()).toBeVisible();
    await row.dispatchEvent("pointerdown", { pointerType: "touch", isPrimary: true, button: 0 });
    await expect(page.locator("html")).not.toHaveAttribute("data-pointer-mode", "mouse");
    await expect(row.locator(".message-actions")).toHaveCSS("width", "1px");
    await row.dispatchEvent("pointerup", { pointerType: "touch", isPrimary: true, button: 0 });
  });
}

test("trackpad on a touch-first device can reply and react in the thread pane", async ({
  page,
}) => {
  const x = randomUUID().slice(0, 8);
  const ws = (await (
    await page.request.post("/api/workspaces", { data: { name: `Pad thread ${x}` } })
  ).json()) as { workspace: Workspace };
  const ch = (await (
    await page.request.post(`/api/workspaces/${ws.workspace.id}/channels`, {
      data: { name: `pad-thread-${x}`, kind: "public" },
    })
  ).json()) as { channel: Channel };
  const posted = await page.request.post(`/api/channels/${ch.channel.id}/messages`, {
    data: { body: `Pad thread root ${x}` },
  });
  expect(posted.ok()).toBe(true);
  const root = ((await posted.json()) as { message: Message }).message;
  const replied = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
    data: { body: `Pad thread reply ${x}` },
  });
  expect(replied.ok()).toBe(true);
  const reply = ((await replied.json()) as { message: Message }).message;
  await page.goto(`/app/${ws.workspace.route_id}/${ch.channel.route_id}`);
  await waitForAppReady(page);
  await expect(page.locator(".messages.is-revealing")).toHaveCount(0);
  const rootRow = page.locator(`.message-row[data-message-id="${root.id}"]`);
  await rootRow.hover();
  await expect(page.locator("html")).toHaveAttribute("data-pointer-mode", "mouse");
  await rootRow.getByRole("button", { name: "Open thread", exact: true }).click();
  const threadPane = page.getByRole("complementary", { name: "Thread pane" });
  await expect(threadPane).toBeVisible();
  const quote = threadPane.locator(".reply-composer").getByLabel("Replying to message");
  for (const target of [root, reply]) {
    const message = threadPane.locator(`[data-message-id="${target.id}"]`);
    await expect(message).toBeVisible();
    await message.hover();
    const replyButton = message.getByRole("button", { name: "Reply", exact: true });
    const addReaction = message.getByRole("button", { name: "Add reaction" });
    await expect(replyButton).toBeVisible();
    await expect(addReaction).toBeVisible();
    expect((await replyButton.boundingBox())!.width).toBeGreaterThan(12);
    expect((await addReaction.boundingBox())!.width).toBeGreaterThan(12);
    await replyButton.click();
    await expect(quote).toContainText(target.body);
  }
});
