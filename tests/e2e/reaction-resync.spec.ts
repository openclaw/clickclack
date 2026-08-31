import { expect, test, type WebSocketRoute } from "@playwright/test";
import { threadFixture, longThread } from "./thread-fixture";

for (const surface of ["channel embed", "thread embed", "full app"] as const) {
  test(`${surface} replaces root and reply reactions on authoritative resync`, async ({ page }) => {
    test.setTimeout(90_000);
    let socket: WebSocketRoute | undefined, server: WebSocketRoute | undefined;
    let connections = 0,
      muted = false;
    await page.routeWebSocket("**/api/realtime/ws?*", (client) => {
      socket = client;
      server = client.connectToServer();
      connections++;
      server.onMessage((data) => {
        const event = JSON.parse(String(data));
        if (muted && event.type.startsWith("reaction.")) return;
        client.send(data);
      });
    });
    const replyCount = surface === "full app" ? 228 : 128;
    const fixture =
      surface === "channel embed"
        ? { ...(await threadFixture(page)), replies: [] }
        : await longThread(page, replyCount);
    const { workspace, channel, roots, replies } = fixture;
    if (surface === "channel embed")
      await page.goto(`/embed/channel/${workspace.route_id}/${channel.route_id}`);
    if (surface === "thread embed")
      await page.goto(`/embed/thread/${workspace.route_id}/${roots[0].route_id}`);
    if (surface !== "channel embed") {
      const rows = page.locator(".reply-list .reply");
      await expect(rows).toHaveCount(100);
      while ((await rows.count()) < replyCount) {
        const nextCount = Math.min(replyCount, (await rows.count()) + 50);
        await page.getByRole("button", { name: "Load older replies", exact: true }).click();
        await expect(rows).toHaveCount(nextCount);
      }
    }
    const scope =
      surface === "channel embed"
        ? page.getByLabel("Embedded channel", { exact: true })
        : page.locator(".thread.open");
    const targets =
      surface === "channel embed" ? roots : [roots[0], replies[0], replies[replyCount - 18]];
    const cursorKey = `clickclack:${workspace.id}:cursor`;
    const tail = await page.request.get(
      `/api/realtime/events?workspace_id=${workspace.id}&include_tail=true&limit=1`,
    );
    const { tail_cursor } = await tail.json();
    const waitForCursor = async (target: string) => {
      await expect
        .poll(async () => {
          const cursor = await page.evaluate((key) => localStorage.getItem(key), cursorKey);
          return Boolean(cursor && cursor >= target);
        })
        .toBe(true);
    };
    await waitForCursor(tail_cursor);
    for (const target of targets)
      await expect(scope.locator(`[data-message-id="${target.id}"]`)).toBeAttached();
    for (const action of ["add", "remove"] as const) {
      muted = true;
      let cursor = "";
      for (const target of targets) {
        const path = `/api/messages/${target.id}/reactions`;
        const response =
          action === "add"
            ? await page.request.post(path, { data: { emoji: "👍" } })
            : await page.request.delete(`${path}/${encodeURIComponent("👍")}`);
        expect(response.ok()).toBe(true);
        const { event } = await response.json();
        cursor = event.cursor;
        const reaction = scope
          .locator(`[data-message-id="${target.id}"]`)
          .getByRole("button", { name: "👍 — 1 reaction", exact: true });
        if (action === "add") await expect(reaction).toHaveCount(0);
        else await expect(reaction).toHaveAttribute("aria-pressed", "true");
      }
      expect(cursor).toBeTruthy();
      const previousConnections = connections;
      const upstream = server!;
      await socket!.close({ code: 4001, reason: "Authoritative resync regression" });
      await upstream.close();
      await expect.poll(() => connections).toBeGreaterThan(previousConnections);
      await waitForCursor(cursor);
      for (const target of targets) {
        const reaction = scope
          .locator(`[data-message-id="${target.id}"]`)
          .getByRole("button", { name: "👍 — 1 reaction", exact: true });
        if (action === "add") await expect(reaction).toHaveAttribute("aria-pressed", "true");
        else await expect(reaction).toHaveCount(0);
      }
      muted = false;
      const target = targets.at(-1)!;
      const live = await page.request.post(`/api/messages/${target.id}/reactions`, {
        data: { emoji: "👀" },
      });
      expect(live.ok()).toBe(true);
      await expect(
        scope
          .locator(`[data-message-id="${target.id}"]`)
          .getByRole("button", { name: "👀 — 1 reaction", exact: true }),
      ).toHaveAttribute("aria-pressed", "true");
      expect(
        (
          await page.request.delete(
            `/api/messages/${target.id}/reactions/${encodeURIComponent("👀")}`,
          )
        ).ok(),
      ).toBe(true);
      await expect(
        scope.locator(`[data-message-id="${target.id}"]`).getByRole("button", { name: /👀 —/ }),
      ).toHaveCount(0);
    }
  });
}
