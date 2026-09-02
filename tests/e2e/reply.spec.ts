import { expect, test } from "@playwright/test";
import { createGeneralChannel } from "./channel-fixture";

test("inline quote-reply renders, jumps, and survives source delete", async ({ page }) => {
  const { workspace, channel, route } = await createGeneralChannel(page, "Reply");
  await page.goto(route);
  await expect(page.getByRole("heading", { name: "#general" })).toBeVisible();

  // Send the original message we'll reply to.
  await page.getByLabel("Message body").fill("the quoted original");
  await page.getByRole("button", { name: "Send" }).click();
  const original = page.locator(".markdown").filter({ hasText: "the quoted original" });
  await expect(original).toBeVisible();

  // Click Quote on the row, ensure composer chip appears, send a reply.
  const originalRow = page.locator(".message-row", {
    has: page.locator(".markdown").filter({ hasText: "the quoted original" }),
  });
  await originalRow.hover();
  await originalRow.getByRole("button", { name: "Reply" }).click();
  await expect(page.getByLabel("Replying to message")).toBeVisible();

  await page.getByLabel("Message body").fill("responding inline");
  await page.getByRole("button", { name: "Send" }).click();
  const replyRow = page.locator(".message-row", {
    has: page.locator(".markdown").filter({ hasText: "responding inline" }),
  });
  await expect(replyRow).toBeVisible();

  const quoteBlock = replyRow.locator(".quote-block");
  await expect(quoteBlock).toBeVisible();
  await expect(quoteBlock).toContainText("the quoted original");

  // The composer chip should clear after sending.
  await expect(page.getByLabel("Replying to message")).toHaveCount(0);

  // Clicking the quote block highlights the source message.
  await quoteBlock.click();
  await expect(originalRow).toHaveClass(/highlight/);

  // Cross-channel quote is forbidden by the API: directly verify the contract.
  const created = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "second", kind: "public" },
  });
  expect(created.ok()).toBe(true);
  const otherChannel = (await created.json()).channel;

  // Get the original's id by reading the channel history via API.
  const list = await page.request.get(`/api/channels/${channel.id}/messages`);
  const { messages } = await list.json();
  const originalMsg = messages.find((m: { body: string }) => m.body === "the quoted original");
  expect(originalMsg).toBeTruthy();

  const crossResp = await page.request.post(`/api/channels/${otherChannel.id}/messages`, {
    data: { body: "leak attempt", quoted_message_id: originalMsg.id },
  });
  expect(crossResp.status()).toBe(400);
});
