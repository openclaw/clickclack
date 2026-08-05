import { build, files, version } from "$service-worker";

const CACHE_PREFIX = "clickclack-web";
const SHELL_CACHE = `${CACHE_PREFIX}-${version}`;
const API_CACHE = `${CACHE_PREFIX}-api-${version}`;
const PRECACHE_URLS = [...new Set([...build, ...files, "/200.html"])];
const sw = self;

sw.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(SHELL_CACHE);
      await cache.addAll(PRECACHE_URLS);
      await sw.skipWaiting();
    })(),
  );
});

sw.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const cacheNames = await caches.keys();
      await Promise.all(
        cacheNames
          .filter((cacheName) => cacheName.startsWith(CACHE_PREFIX) && cacheName !== SHELL_CACHE && cacheName !== API_CACHE)
          .map((cacheName) => caches.delete(cacheName)),
      );
      await sw.clients.claim();
    })(),
  );
});

sw.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;

  const url = new URL(request.url);

  if (request.mode === "navigate") {
    event.respondWith(
      (async () => {
        try {
          return await fetch(request);
        } catch {
          const cache = await caches.open(SHELL_CACHE);
          const fallback = await cache.match("/200.html");
          if (fallback) return fallback;
          throw new Error("ClickClack navigation fallback is missing");
        }
      })(),
    );
    return;
  }

  if (url.origin === sw.location.origin && url.pathname.startsWith("/api/")) {
    event.respondWith(
      (async () => {
        const cache = await caches.open(API_CACHE);
        try {
          const response = await fetch(request);
          if (response.ok) {
            void cache.put(request, response.clone());
          }
          return response;
        } catch (error) {
          const cached = await cache.match(request);
          if (cached) return cached;
          throw error;
        }
      })(),
    );
    return;
  }

  if (url.origin === sw.location.origin) {
    event.respondWith(
      (async () => {
        const cache = await caches.open(SHELL_CACHE);
        const cached = await cache.match(request);
        if (cached) return cached;
        const response = await fetch(request);
        if (response.ok) {
          void cache.put(request, response.clone());
        }
        return response;
      })(),
    );
  }
});

sw.addEventListener("push", (event) => {
  const payload = event.data ? event.data.json() : {};
  const title = typeof payload.title === "string" && payload.title.trim() ? payload.title : "ClickClack";
  const body = typeof payload.body === "string" ? payload.body : "";
  const url = typeof payload.url === "string" && payload.url.trim() ? payload.url : "/";
  event.waitUntil(
    sw.registration.showNotification(title, {
      body,
      icon: "/icon-192.png",
      badge: "/icon-192.png",
      tag: typeof payload.tag === "string" ? payload.tag : undefined,
      data: { url },
    }),
  );
});

sw.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = typeof event.notification.data?.url === "string" && event.notification.data.url.trim() ? event.notification.data.url : "/";
  event.waitUntil(
    (async () => {
      const targetURL = new URL(url, sw.location.origin).href;
      const clientsList = await sw.clients.matchAll({ type: "window", includeUncontrolled: true });
      for (const client of clientsList) {
        if ("focus" in client && client.url === targetURL) {
          return client.focus();
        }
      }
      return sw.clients.openWindow(url);
    })(),
  );
});
