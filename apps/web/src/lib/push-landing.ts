// A notification tap with no app window open has to start a fresh one, and a
// message posted to it can arrive before the app is listening: the browser
// starts delivering a new page's worker messages once the document is parsed,
// which is before the app has mounted and added its listener. So the worker
// marks the URL it opens instead, and the app takes the mark on boot and lands
// at the newest message, the same as a tap on an open window. Pure helpers
// with no imports, shared by the worker and the app.

const LANDING_PARAM = "from";
const LANDING_VALUE = "push";

// pushLandingURL is the app path a tap opens a fresh window at.
export function pushLandingURL(path: string): string {
  const url = new URL(path, "https://clickclack.invalid");
  url.searchParams.set(LANDING_PARAM, LANDING_VALUE);
  return `${url.pathname}${url.search}${url.hash}`;
}

// takePushLanding answers the path without the mark when a tap opened this
// page, and null otherwise.
export function takePushLanding(href: string): string | null {
  const url = new URL(href, "https://clickclack.invalid");
  if (url.searchParams.get(LANDING_PARAM) !== LANDING_VALUE) return null;
  url.searchParams.delete(LANDING_PARAM);
  return `${url.pathname}${url.search}${url.hash}`;
}
