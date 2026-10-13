import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { waitForAppReady } from "./app-ready";

// The typing and agent-responding indicators live in a band the composer dock
// reserves permanently. Showing one must never resize the transcript (which
// clips its last line) or paint over the last message.

type Setup = {
  workspaceID: string;
  workspaceRoute: string;
  channelID: string;
  channelRoute: string;
  botToken: string;
};

const FILLER =
  "the quick brown fox jumps over the lazy dog and keeps going so this line wraps on a narrow screen. Final words sit at the very bottom.";

async function setup(page: Page): Promise<Setup> {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 10);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Indicator Band ${suffix}` },
  });
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `band-${suffix}`, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; route_id: string };
  };
  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: "Blackbird",
      handle: `blackbird-${suffix}`,
      token_name: "e2e",
      scopes: ["bot:write"],
    },
  });
  expect(botResponse.status()).toBe(201);
  const bot = (await botResponse.json()) as { bot_token: { token: string } };
  return {
    workspaceID: workspace.id,
    workspaceRoute: workspace.route_id,
    channelID: channel.id,
    channelRoute: channel.route_id,
    botToken: bot.bot_token.token,
  };
}

async function publish(page: Page, s: Setup, type: string, payload: Record<string, unknown>) {
  const response = await page.request.post("/api/realtime/ephemeral", {
    headers: { Authorization: `Bearer ${s.botToken}` },
    data: { workspace_id: s.workspaceID, channel_id: s.channelID, type, payload },
  });
  expect(response.status()).toBe(202);
}

function measure(page: Page, scrollSelector: string, indicatorSelector: string) {
  return page.evaluate(
    ([scrollSel, indicatorSel]) => {
      const scroll = document.querySelector(scrollSel)!;
      const bodies = scroll.querySelectorAll(".markdown");
      const last = bodies[bodies.length - 1].getBoundingClientRect();
      const indicator = document.querySelector(indicatorSel)?.getBoundingClientRect();
      return {
        scrollHeight: scroll.getBoundingClientRect().height,
        distanceFromBottom: scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight,
        lastTextBottom: last.bottom,
        indicatorTop: indicator?.top ?? null,
      };
    },
    [scrollSelector, indicatorSelector],
  );
}

for (const viewport of [
  { name: "desktop", width: 1280, height: 800 },
  { name: "phone", width: 390, height: 844 },
]) {
  test(`typing indicator keeps clear of the last channel message (${viewport.name})`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    const s = await setup(page);
    for (let i = 0; i < 16; i++) {
      const response = await page.request.post(`/api/channels/${s.channelID}/messages`, {
        data: { body: `Message ${i}: ${FILLER}` },
      });
      expect(response.ok()).toBe(true);
    }
    await page.goto(`/app/${s.workspaceRoute}/${s.channelRoute}`);
    await waitForAppReady(page);
    await expect(page.locator(".markdown").filter({ hasText: "Message 15:" })).toBeVisible();
    await expect
      .poll(async () => (await measure(page, ".messages-scroll", "main .none")).distanceFromBottom)
      .toBeLessThanOrEqual(1);
    const idle = await measure(page, ".messages-scroll", "main .none");

    await publish(page, s, "typing.started", {});
    const indicator = page.locator("main .typing-indicator.visible:not(.agent-responding)");
    await expect(indicator).toHaveText(/Blackbird is typing/);
    await expect.poll(() => indicator.evaluate((el) => getComputedStyle(el).opacity)).toBe("1");

    const active = await measure(
      page,
      ".messages-scroll",
      "main .typing-indicator.visible:not(.agent-responding)",
    );
    expect(active.scrollHeight).toBe(idle.scrollHeight);
    expect(active.distanceFromBottom).toBeLessThanOrEqual(1);
    expect(active.indicatorTop).not.toBeNull();
    expect(active.indicatorTop!).toBeGreaterThanOrEqual(active.lastTextBottom);
  });

  test(`responding indicator keeps clear of the last thread reply (${viewport.name})`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    const s = await setup(page);
    const rootResponse = await page.request.post(`/api/channels/${s.channelID}/messages`, {
      data: { body: "Thread root" },
    });
    const root = (await rootResponse.json()) as { message: { id: string } };
    for (let i = 0; i < 14; i++) {
      const response = await page.request.post(`/api/messages/${root.message.id}/thread/replies`, {
        data: { body: `Reply ${i}: ${FILLER}` },
      });
      expect(response.ok()).toBe(true);
    }
    await page.goto(`/app/${s.workspaceRoute}/${s.channelRoute}`);
    await waitForAppReady(page);
    const rootRow = page.locator(`[data-message-id="${root.message.id}"]`).first();
    await rootRow.hover();
    await rootRow
      .getByRole("button", { name: /thread|repl/i })
      .first()
      .click();
    const pane = page.getByRole("complementary", { name: "Thread pane" });
    await expect(pane.getByText("Reply 13:")).toBeVisible();
    await expect
      .poll(async () => (await measure(page, ".thread .thread-scroll", ".none")).distanceFromBottom)
      .toBeLessThanOrEqual(1);
    const idle = await measure(page, ".thread .thread-scroll", ".none");

    await publish(page, s, "agent.progress", {
      turn_id: `turn-${root.message.id}`,
      seq: 1,
      op: "append",
      line: { id: "line-1", kind: "commentary", text: "Working", status: "running" },
    });
    await expect(pane.locator(".agent-responding .typing-indicator__label")).toHaveText(
      "Blackbird is responding…",
    );
    await expect
      .poll(() => pane.locator(".agent-responding").evaluate((el) => getComputedStyle(el).opacity))
      .toBe("1");

    const active = await measure(page, ".thread .thread-scroll", ".thread .agent-responding");
    expect(active.scrollHeight).toBe(idle.scrollHeight);
    expect(active.distanceFromBottom).toBeLessThanOrEqual(1);
    expect(active.indicatorTop!).toBeGreaterThanOrEqual(active.lastTextBottom);
  });
}
