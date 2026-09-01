import type { Page } from "@playwright/test";

export async function settleScrollFrames(page: Page) {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        let frames = 12;
        const step = () => {
          frames--;
          if (frames <= 0) {
            resolve();
            return;
          }
          requestAnimationFrame(step);
        };
        requestAnimationFrame(step);
      }),
  );
}

export async function pauseMessageFrames(page: Page) {
  await page.evaluate(() => {
    const request = window.requestAnimationFrame;
    const cancel = window.cancelAnimationFrame;
    const pending = new Map<number, FrameRequestCallback>();
    let nextID = -1;
    window.requestAnimationFrame = (callback) => {
      const id = nextID--;
      pending.set(id, callback);
      return id;
    };
    window.cancelAnimationFrame = (id) => {
      if (!pending.delete(id)) cancel(id);
    };
    Reflect.set(window, "resumeMessageFrames", () => {
      window.requestAnimationFrame = request;
      window.cancelAnimationFrame = cancel;
      for (const callback of pending.values()) request(callback);
      pending.clear();
      Reflect.deleteProperty(window, "resumeMessageFrames");
    });
  });
}
