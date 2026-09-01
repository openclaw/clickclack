import { expect, test, type CDPSession } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";
import { pauseMessageFrames, settleScrollFrames } from "./message-frames";

for (const input of ["wheel", "keyboard", "touch"] as const) {
  test(`a ${input} scroll cancels a pending live follow`, async ({ page }) => {
    const workspaceResponse = await page.request.post("/api/workspaces", {
      data: { name: `Follow cancellation ${randomUUID()}` },
    });
    expect(workspaceResponse.ok()).toBe(true);
    const { workspace } = (await workspaceResponse.json()) as {
      workspace: { id: string; route_id: string };
    };
    const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
      data: { name: "general", kind: "public" },
    });
    expect(channelResponse.ok()).toBe(true);
    const { channel } = (await channelResponse.json()) as {
      channel: { id: string; route_id: string };
    };
    const messagesPath = `/api/channels/${channel.id}/messages`;
    const seed = await page.request.post(messagesPath, {
      data: {
        body:
          "History\n\n" +
          (
            "Earlier conversation with enough text to wrap as the viewport changes. ".repeat(3) +
            "\n\n"
          ).repeat(40),
      },
    });
    expect(seed.ok()).toBe(true);
    await page.goto(`/app/${workspace.route_id}/${channel.route_id}`);
    await waitForAppReady(page);
    await settleScrollFrames(page);
    const scroller = page.locator(".messages-scroll");
    const bottomGap = () =>
      scroller.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
    await expect.poll(bottomGap).toBeLessThanOrEqual(2);

    let touchSession: CDPSession | undefined;
    await pauseMessageFrames(page);
    try {
      const appended = await page.request.post(messagesPath, {
        data: { body: "Live append\n\n" + "Growing the existing group.\n\n".repeat(16) },
      });
      expect(appended.ok()).toBe(true);
      await expect(page.getByText("Live append", { exact: true })).toBeVisible();
      if (input === "wheel") {
        const box = await scroller.boundingBox();
        if (!box) throw new Error("message viewport missing");
        await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
        await page.mouse.wheel(0, -500);
      } else if (input === "keyboard") {
        await scroller.focus();
        await page.keyboard.press("Home");
      } else {
        const box = await scroller.boundingBox();
        if (!box) throw new Error("message viewport missing");
        touchSession = await page.context().newCDPSession(page);
        await touchSession.send("Emulation.setTouchEmulationEnabled", { enabled: true });
        const x = box.x + box.width / 2;
        await touchSession.send("Input.dispatchTouchEvent", {
          type: "touchStart",
          touchPoints: [{ x, y: box.y + 40 }],
        });
        for (const distance of [100, 200, 300, 400]) {
          await touchSession.send("Input.dispatchTouchEvent", {
            type: "touchMove",
            touchPoints: [{ x, y: box.y + 40 + distance }],
          });
        }
      }
      await expect.poll(bottomGap).toBeGreaterThan(300);
      // Reflow the same virtual group while the canceled follow still awaits a frame.
      const heightBeforeResize = await scroller.evaluate((el) => el.scrollHeight);
      await page.setViewportSize({ width: 1100, height: 720 });
      await expect
        .poll(() => scroller.evaluate((el) => el.scrollHeight))
        .toBeGreaterThan(heightBeforeResize);
      await expect.poll(bottomGap).toBeGreaterThan(300);
    } finally {
      await page.evaluate(() => Reflect.get(window, "resumeMessageFrames")());
    }
    await settleScrollFrames(page);
    await expect.poll(bottomGap).toBeGreaterThan(300);
    if (touchSession) {
      await touchSession.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
      await touchSession.detach();
    }
  });
}
