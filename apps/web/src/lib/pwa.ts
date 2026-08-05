const APP_HOST_PREFIX = "app.";

export async function registerClickClackServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!("serviceWorker" in navigator)) return null;
  if (!window.isSecureContext) {
    if (window.location.hostname.startsWith(APP_HOST_PREFIX)) {
      console.warn("ClickClack service worker requires a trusted HTTPS origin.");
    }
    return null;
  }
  if (!window.location.hostname.startsWith(APP_HOST_PREFIX)) return null;

  try {
    return await navigator.serviceWorker.register("/service-worker.js", { scope: "/" });
  } catch (error) {
    console.error("ClickClack service worker registration failed:", error);
    return null;
  }
}
