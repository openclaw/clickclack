import { expect, test } from "@playwright/test";
import { threadFixture, openThread, expectInsideThread, scrollThreadTo } from "./thread-fixture";

for (const surface of ["main", "embed"] as const) {
  test(`${surface} long threads keep latest replies, reachable history and quote targets`, async ({
    page,
  }) => {
    test.setTimeout(90_000);
    const { roots, workspace } = await threadFixture(page);
    const root = roots[0];
    const post = async (body: string, quote?: string) => {
      const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
        data: { body, quoted_message_id: quote },
      });
      expect(response.ok()).toBe(true);
      return (await response.json()).message;
    };
    const seeded = [];
    for (let n = 2; n <= 102; n++) seeded.push(await post(`History reply ${n}`));
    if (surface === "embed")
      await page.goto(`/embed/thread/${workspace.route_id}/${root.route_id}`);
    else await openThread(page, root.id);
    const row = (id: string) => page.locator(`.reply[data-message-id="${id}"]`);
    await expect(row(seeded.at(-1).id)).toBeVisible();
    await expectInsideThread(row(seeded.at(-1).id), page);
    await expect(page.locator(".reply-list .reply")).toHaveCount(100);
    await page.getByRole("button", { name: "Load older replies", exact: true }).click();
    await expect(page.locator(".reply-list .reply")).toHaveCount(102);
    const anchor = row(seeded[15].id);
    await scrollThreadTo(anchor, page);
    await expect(page.getByRole("button", { name: "Jump to latest", exact: true })).toBeVisible();
    const before = (await anchor.boundingBox())!.y;
    const remote = await post("Remote reply 103");
    await expect(row(remote.id)).toBeAttached();
    await expect(page.locator(".reply-list .reply")).toHaveCount(103);
    await expect
      .poll(async () => Math.abs((await anchor.boundingBox())!.y - before))
      .toBeLessThan(3);
    await page.getByLabel("Reply body").fill("Own reply 104");
    await page
      .locator(".reply-composer")
      .getByRole("button", { name: "Reply", exact: true })
      .click();
    const own = page.locator(".reply").filter({ hasText: "Own reply 104" });
    await expectInsideThread(own, page);
    const later = await post("Remote reply 105");
    await expect(row(later.id)).toBeAttached();
    await expect(own).toHaveCount(1);
    await page.reload();
    await expect(own).toHaveCount(1);
    await expectInsideThread(row(later.id), page);
    const quoted = await post("Quote outside the latest window", seeded[0].id);
    await expect(row(quoted.id)).toBeAttached();
    await row(quoted.id)
      .getByRole("button", { name: /Jump to quoted message/ })
      .click();
    await expectInsideThread(row(seeded[0].id), page);
    await page.getByRole("button", { name: "Jump to latest", exact: true }).click();
    await expectInsideThread(row(quoted.id), page);
  });
}
