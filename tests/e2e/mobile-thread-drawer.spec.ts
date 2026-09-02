import { expect, test } from "@playwright/test";
import { threadFixture, openThread } from "./thread-fixture";

// The thread drawer becomes a fixed overlay on narrow viewports. Both
// regressions here only reproduce at phone width: message-row z-lifts
// painting through the drawer, and the fixed nav toggle covering the
// drawer header.
test.use({ viewport: { width: 375, height: 812 } });

test("thread drawer paints above lifted message rows", async ({ page }) => {
  const { roots } = await threadFixture(page);
  const root = roots[0];

  await openThread(page, root.id);
  const drawer = page.locator(".thread.open");
  await expect(drawer).toBeVisible();
  // Let the slide-in transition settle before sampling paint order.
  await expect
    .poll(() => page.evaluate(() => document.querySelector(".thread")?.getBoundingClientRect().x))
    .toBe(0);

  // Opening a thread marks its root row selected, which lifts the row for
  // its hover toolbar. Without a stacking context on .timeline that lift
  // competed globally and painted through the drawer.
  const selectedLift = await page.evaluate(() => {
    const row = document.querySelector(".message-row.selected");
    return row ? getComputedStyle(row).zIndex : null;
  });
  expect(Number(selectedLift)).toBeGreaterThan(0);

  const leaks = await page.evaluate(() => {
    const hits: string[] = [];
    for (const y of [200, 320, 440, 560, 650]) {
      for (const x of [60, 187, 310]) {
        const top = document.elementsFromPoint(x, y)[0];
        if (top && !top.closest(".thread")) {
          hits.push(`${x},${y} -> ${top.tagName}.${(top.className + "").slice(0, 40)}`);
        }
      }
    }
    return hits;
  });
  expect(leaks).toEqual([]);
});

test("drawer header clears the fixed mobile nav toggle", async ({ page }) => {
  const { roots } = await threadFixture(page);
  const root = roots[0];

  await openThread(page, root.id);
  await expect(page.locator(".thread.open")).toBeVisible();
  // Measure only after the slide-in transition lands, or the header boxes
  // are sampled mid-flight.
  await expect
    .poll(() => page.evaluate(() => document.querySelector(".thread")?.getBoundingClientRect().x))
    .toBe(0);

  const toggle = page.locator(".mobile-nav-toggle");
  await expect(toggle).toBeVisible();
  const toggleBox = await toggle.boundingBox();
  const titleBox = await page.locator(".thread > header > div").boundingBox();
  if (!toggleBox || !titleBox) throw new Error("missing toggle or header title box");

  const overlaps =
    titleBox.x < toggleBox.x + toggleBox.width &&
    titleBox.x + titleBox.width > toggleBox.x &&
    titleBox.y < toggleBox.y + toggleBox.height &&
    titleBox.y + titleBox.height > toggleBox.y;
  expect(overlaps).toBe(false);
});
