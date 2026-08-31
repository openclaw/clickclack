import { expect, test } from "@playwright/test";
import { deferred, threadFixture, openThread, expectInsideThread } from "./thread-fixture";

test("rich native history stays bounded, preserves reading anchors and reloads trimmed edges", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const { roots, workspace } = await threadFixture(page);
  const root = roots[0];
  const imageResponse = await page.request.post(`/api/uploads?workspace_id=${workspace.id}`, {
    multipart: {
      file: {
        name: "thread-history.png",
        mimeType: "image/png",
        buffer: Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=",
          "base64",
        ),
      },
    },
  });
  expect(imageResponse.ok()).toBe(true);
  const image = (await imageResponse.json()).upload;
  const replies = [];
  for (let n = 2; n <= 360; n++) {
    const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
      data: {
        body: `### Rich reply ${n}\n\n**Bold** and _emphasis_, [a link](https://example.com).\n\n> Quoted context\n\n\`\`\`typescript\nconst reply = ${n};\n\`\`\`\n\n| Name | Value |\n| --- | --- |\n| Reply | ${n} |`,
        quoted_message_id: n % 10 === 0 ? root.id : undefined,
      },
    });
    expect(response.ok()).toBe(true);
    replies.push((await response.json()).message);
  }
  // An attachment is served by the real upload owner; only its delivery is delayed.
  await page.request.post(`/api/messages/${replies.at(-1).id}/attachments`, {
    data: { upload_id: image.id },
  });
  const mediaEntered = deferred(),
    releaseMedia = deferred();
  await page.route(`**/api/uploads/${image.id}`, async (route) => {
    const response = await route.fetch();
    mediaEntered.resolve();
    await releaseMedia.promise;
    await route.fulfill({ response });
  });
  try {
    await openThread(page, root.id);
    await expect(page.locator(".reply-list .reply")).toHaveCount(100);
    await mediaEntered.promise;
    releaseMedia.resolve();
    const last = page.locator(`.reply[data-message-id="${replies.at(-1).id}"]`);
    await expect
      .poll(() =>
        last
          .locator("img")
          .last()
          .evaluate((node: HTMLImageElement) => node.complete && node.naturalWidth > 0),
      )
      .toBe(true);
    await expect
      .poll(() =>
        page
          .locator(".thread-scroll")
          .evaluate((node) => node.scrollHeight - node.clientHeight - node.scrollTop),
      )
      .toBeLessThan(3);

    for (const count of [150, 200, 250, 300]) {
      await page.getByRole("button", { name: "Load older replies", exact: true }).click();
      await expect(page.locator(".reply-list .reply")).toHaveCount(count);
    }
    // Measure native layout with 300 rich rows, without relying on the channel virtualizer.
    const frames = await page.evaluate(async () => {
      const intervals: number[] = [];
      let previous = performance.now();
      for (let n = 0; n < 24; n++)
        await new Promise<void>((resolve) =>
          requestAnimationFrame((now) => {
            intervals.push(now - previous);
            previous = now;
            resolve();
          }),
        );
      return {
        rows: document.querySelectorAll(".reply-list .reply").length,
        nodes: document.querySelectorAll(".reply-list *").length,
        maxFrameMs: Math.max(...intervals),
      };
    });
    console.info("Native rich thread window", JSON.stringify(frames));
    expect(frames.rows).toBe(300);
    expect(frames.maxFrameMs).toBeLessThan(1000);
    await page.getByLabel("Reply body").fill("Retain this history draft");
    await expect(page.getByLabel("Reply body")).toHaveValue("Retain this history draft");

    await page.getByRole("button", { name: "Load older replies", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Load newer replies", exact: true }),
    ).toBeAttached();
    await expect.poll(() => page.locator(".reply-list .reply").count()).toBeLessThanOrEqual(300);
    await page.getByRole("button", { name: "Load older replies", exact: true }).click();
    await expect(page.locator(".reply-list")).toContainText("First existing reply");
    const anchor = page.locator(`.reply[data-message-id="${replies[99].id}"]`);
    await anchor.evaluate((node) => node.scrollIntoView({ block: "start" }));
    await expect(page.getByRole("button", { name: "Jump to latest", exact: true })).toBeVisible();
    const y = (await anchor.boundingBox())!.y;
    for (let n = 361; n <= 368; n++) {
      const response = await page.request.post(`/api/messages/${root.id}/thread/replies`, {
        data: { body: `Live burst ${n}` },
      });
      expect(response.ok()).toBe(true);
    }
    await expect(page.locator(".thread > header strong").first()).toContainText("368 replies");
    await expect.poll(async () => Math.abs((await anchor.boundingBox())!.y - y)).toBeLessThan(3);
    await expect.poll(() => page.locator(".reply-list .reply").count()).toBeLessThanOrEqual(300);
    await expect(page.getByLabel("Reply body")).toHaveValue("Retain this history draft");

    // The discarded tail is reachable through the same edge control, without a middle gap.
    const newer = page.getByRole("button", { name: "Load newer replies", exact: true });
    await newer.scrollIntoViewIfNeeded();
    const edgeAnchor = page.locator(".reply-list .reply").last();
    const edgeID = await edgeAnchor.getAttribute("data-message-id");
    const edgeY = (await edgeAnchor.boundingBox())!.y;
    await newer.click();
    await expect(page.locator(`.reply[data-message-id="${replies[249].id}"]`)).toBeAttached();
    await expect
      .poll(async () =>
        Math.abs(
          (await page.locator(`.reply[data-message-id="${edgeID}"]`).boundingBox())!.y - edgeY,
        ),
      )
      .toBeLessThan(3);
    await page.getByRole("button", { name: "Jump to latest", exact: true }).click();
    await expectInsideThread(page.locator(".reply").filter({ hasText: "Live burst 368" }), page);
    await expect(page.locator(".reply-list .reply")).toHaveCount(100);
    await expect(
      page.getByRole("button", { name: "Load older replies", exact: true }),
    ).toBeAttached();
  } finally {
    releaseMedia.resolve();
  }
});
