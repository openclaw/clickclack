import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { BotToken } from "../../apps/web/src/lib/bots";
import type { Channel, DirectConversation, User, Workspace } from "../../apps/web/src/lib/types";
import { waitForAppReady } from "./app-ready";

// A running animation produces a frame every display refresh, so an idle
// conversation must have none, including on elements that are mounted but hidden.

type Fixture = {
  workspace: Workspace;
  channel: Channel;
  dm: DirectConversation;
  botToken: string;
};

async function fixture(page: Page): Promise<Fixture> {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 10);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `Idle ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace }: { workspace: Workspace } = await workspaceResponse.json();
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: `idle-${suffix}`, kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel }: { channel: Channel } = await channelResponse.json();
  const botResponse = await page.request.post(`/api/workspaces/${workspace.id}/bots`, {
    data: {
      display_name: "Blackbird",
      handle: `blackbird-${suffix}`,
      token_name: "e2e",
      scopes: ["bot:write"],
    },
  });
  expect(botResponse.status()).toBe(201);
  const { bot, bot_token }: { bot: User; bot_token: BotToken } = await botResponse.json();
  if (!bot_token.token) throw new Error("bot token missing");
  const dmResponse = await page.request.post("/api/dms", {
    data: { workspace_id: workspace.id, member_ids: [bot.id] },
  });
  expect(dmResponse.ok()).toBe(true);
  const { conversation: dm }: { conversation: DirectConversation } = await dmResponse.json();
  return { workspace, channel, dm, botToken: bot_token.token };
}

async function post(page: Page, path: string, body: string) {
  const response = await page.request.post(path, { data: { body } });
  expect(response.ok()).toBe(true);
}

async function publishTyping(page: Page, s: Fixture, type: "typing.started" | "typing.stopped") {
  const response = await page.request.post("/api/realtime/ephemeral", {
    headers: { Authorization: `Bearer ${s.botToken}` },
    data: { workspace_id: s.workspace.id, channel_id: s.channel.id, type, payload: {} },
  });
  expect(response.status()).toBe(202);
}

function runningAnimations(page: Page) {
  return page.evaluate(() =>
    document
      .getAnimations()
      .filter((animation) => animation.playState === "running")
      .map((animation) => {
        const effect = animation.effect instanceof KeyframeEffect ? animation.effect : null;
        const target = effect?.target;
        const host = target?.classList.length ? target : target?.parentElement;
        const name =
          animation instanceof CSSAnimation
            ? animation.animationName
            : animation instanceof CSSTransition
              ? animation.transitionProperty
              : animation.id;
        return `${name} on .${host?.classList[0]}${effect?.pseudoElement ?? ""}`;
      }),
  );
}

for (const surface of ["channel", "DM"] as const) {
  test(`an idle ${surface} runs no animations`, async ({ page }) => {
    const s = await fixture(page);
    const path = surface === "channel" ? `/api/channels/${s.channel.id}` : `/api/dms/${s.dm.id}`;
    await post(page, `${path}/messages`, `Settled ${surface}`);
    await page.goto(
      `/app/${s.workspace.route_id}/${surface === "channel" ? s.channel.route_id : s.dm.route_id}`,
    );
    await waitForAppReady(page);
    await expect(page.locator(".markdown").filter({ hasText: `Settled ${surface}` })).toBeVisible();
    await expect.poll(() => runningAnimations(page)).toEqual([]);
  });
}

test("typing dots run only while someone is typing", async ({ page }) => {
  const s = await fixture(page);
  await post(page, `/api/channels/${s.channel.id}/messages`, "Before typing");
  await page.goto(`/app/${s.workspace.route_id}/${s.channel.route_id}`);
  await waitForAppReady(page);
  await expect(page.locator(".markdown").filter({ hasText: "Before typing" })).toBeVisible();
  const indicator = page.locator("main .typing-indicator:not(.agent-responding)");

  await publishTyping(page, s, "typing.started");
  await expect(indicator).toHaveText(/Blackbird is typing/);
  await expect
    .poll(() => runningAnimations(page))
    .toEqual(Array(3).fill("typing-dot on .typing-indicator__dots"));

  await publishTyping(page, s, "typing.stopped");
  await expect(indicator).not.toHaveClass(/\bvisible\b/);
  await expect.poll(() => runningAnimations(page)).toEqual([]);
});
