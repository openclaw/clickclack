import { Container, getContainer } from "@cloudflare/containers";
import { env } from "cloudflare:workers";

function optionalContainerEnv(values: Record<string, string | undefined>): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== "") {
      result[key] = value;
    }
  }
  return result;
}

export class ClickClackContainer extends Container {
  defaultPort = 8080;
  sleepAfter = "10m";
  envVars = {
    CLICKCLACK_ADDR: ":8080",
    CLICKCLACK_DATA: "/app/data",
    CLICKCLACK_DB: env.CLICKCLACK_DB,
    CLICKCLACK_UPLOADS: env.CLICKCLACK_UPLOADS ?? "",
    CLICKCLACK_PUBLIC_URL: env.CLICKCLACK_PUBLIC_URL,
    CLICKCLACK_DEV_BOOTSTRAP: "false",
    CLICKCLACK_GITHUB_CLIENT_ID: env.CLICKCLACK_GITHUB_CLIENT_ID,
    CLICKCLACK_GITHUB_CLIENT_SECRET: env.CLICKCLACK_GITHUB_CLIENT_SECRET,
    CLICKCLACK_PUSHOVER_API_TOKEN: env.CLICKCLACK_PUSHOVER_API_TOKEN ?? "",
    CLICKCLACK_R2_ACCOUNT_ID: env.CLICKCLACK_R2_ACCOUNT_ID ?? "",
    CLICKCLACK_R2_ACCESS_KEY_ID: env.CLICKCLACK_R2_ACCESS_KEY_ID ?? "",
    CLICKCLACK_R2_SECRET_ACCESS_KEY: env.CLICKCLACK_R2_SECRET_ACCESS_KEY ?? "",
    CLICKCLACK_R2_ENDPOINT: env.CLICKCLACK_R2_ENDPOINT ?? "",
    ...optionalContainerEnv({
      CLICKCLACK_GITHUB_ALLOWED_ORG: env.CLICKCLACK_GITHUB_ALLOWED_ORG,
      CLICKCLACK_GITHUB_MODERATOR_ORG: env.CLICKCLACK_GITHUB_MODERATOR_ORG,
    }),
  };
}

export default {
  async fetch(request: Request, workerEnv: Env): Promise<Response> {
    const requestURL = new URL(request.url);

    // PROJECT LOGOS: on the standalone logos origin, serve the LOGOS app at
    // the root (NOT a subpath) with same-origin /api + /cognition so cookie
    // auth and cognition token injection keep working. clickclack stays
    // plumbing underneath; LOGOS is the upspun surface.
    const isLogosOrigin =
      requestURL.hostname === "logos.catabolicsolutions.com" ||
      requestURL.hostname.endsWith(".logos.catabolicsolutions.com");
    if (isLogosOrigin) {
      const incoming = new URL(request.url);
      const isApi = incoming.pathname === "/api" || incoming.pathname.startsWith("/api/");
      const isCognition =
        incoming.pathname === "/cognition" || incoming.pathname.startsWith("/cognition/");
      const upstreamBase = isCognition
        ? workerEnv.CLICKCLACK_COGNITION_URL!
        : isApi
          ? workerEnv.CLICKCLACK_UPSTREAM_URL!
          : workerEnv.CLICKCLACK_LOGOS_URL!;
      const upstream = new URL(upstreamBase);
      if (isCognition) {
        upstream.pathname = incoming.pathname.replace(/^\/cognition(\/|$)/, "/");
      } else {
        upstream.pathname = incoming.pathname;
        if (!isApi && incoming.pathname === "/") upstream.pathname = "/";
      }
      upstream.search = incoming.search;

      const headers = new Headers(request.headers);
      headers.set("X-Forwarded-Host", incoming.host);
      headers.set("X-Forwarded-Proto", incoming.protocol.replace(":", ""));
      if (isCognition && workerEnv.CLICKCLACK_COGNITION_TOKEN) {
        headers.set("Authorization", `Bearer ${workerEnv.CLICKCLACK_COGNITION_TOKEN}`);
      }

      return fetch(
        new Request(upstream.toString(), {
          method: request.method,
          headers,
          body: request.method === "GET" || request.method === "HEAD" ? null : request.body,
          redirect: "manual",
        }),
      );
    }

    const shouldProxyToUpstream =
      workerEnv.CLICKCLACK_UPSTREAM_URL &&
      (requestURL.pathname === "/api" || requestURL.pathname.startsWith("/api/"));

    // PROJECT LOGOS: proxy cognition service requests to the cognition
    // service (droplet :8787 by default) — the "brain" for intent/persona/
    // transforms/memory. Falls back to the container when unset.
    const shouldProxyCognition =
      workerEnv.CLICKCLACK_COGNITION_URL &&
      (requestURL.pathname === "/cognition" || requestURL.pathname.startsWith("/cognition/"));

    // PROJECT LOGOS: serve the LOGOS application (droplet :8788) at /logos/*
    // on the same origin so /api auth + /cognition stay same-origin.
    const shouldProxyLogos =
      workerEnv.CLICKCLACK_LOGOS_URL &&
      (requestURL.pathname === "/logos" || requestURL.pathname.startsWith("/logos/"));

    if (shouldProxyToUpstream || shouldProxyCognition || shouldProxyLogos) {
      const incoming = new URL(request.url);
      let upstreamBase = workerEnv.CLICKCLACK_UPSTREAM_URL!;
      if (shouldProxyCognition) upstreamBase = workerEnv.CLICKCLACK_COGNITION_URL!;
      else if (shouldProxyLogos) upstreamBase = workerEnv.CLICKCLACK_LOGOS_URL!;
      const upstream = new URL(upstreamBase);
      if (shouldProxyCognition) {
        // Strip the /cognition prefix — cognition routes are /analyze, /transform, etc.
        upstream.pathname = incoming.pathname.replace(/^\/cognition(\/|$)/, "/");
      } else if (shouldProxyLogos) {
        // Strip the /logos prefix so the static server resolves /logos/ → index
        upstream.pathname = incoming.pathname.replace(/^\/logos(\/|$)/, "/");
        if (incoming.pathname === "/logos") upstream.pathname = "/";
      } else {
        upstream.pathname = incoming.pathname;
      }
      upstream.search = incoming.search;

      const headers = new Headers(request.headers);
      headers.set("X-Forwarded-Host", incoming.host);
      headers.set("X-Forwarded-Proto", incoming.protocol.replace(":", ""));

      // PROJECT LOGOS: inject the shared cognition token on proxied calls so
      // browsers never see it and arbitrary callers get 401 from the service.
      if (shouldProxyCognition && workerEnv.CLICKCLACK_COGNITION_TOKEN) {
        headers.set("Authorization", `Bearer ${workerEnv.CLICKCLACK_COGNITION_TOKEN}`);
      }

      return fetch(
        new Request(upstream.toString(), {
          method: request.method,
          headers,
          body: request.method === "GET" || request.method === "HEAD" ? null : request.body,
          redirect: "manual",
        }),
      );
    }

    const container = getContainer(workerEnv.CLICKCLACK_CONTAINER, workerEnv.CLICKCLACK_CONTAINER_NAME || "prod");
    return container.fetch(request);
  },
};
