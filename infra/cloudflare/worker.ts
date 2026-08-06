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
    const shouldProxyToUpstream =
      workerEnv.CLICKCLACK_UPSTREAM_URL &&
      (requestURL.pathname === "/api" || requestURL.pathname.startsWith("/api/"));

    if (shouldProxyToUpstream) {
      const incoming = new URL(request.url);
      const upstream = new URL(workerEnv.CLICKCLACK_UPSTREAM_URL);
      upstream.pathname = incoming.pathname;
      upstream.search = incoming.search;

      const headers = new Headers(request.headers);
      headers.set("X-Forwarded-Host", incoming.host);
      headers.set("X-Forwarded-Proto", incoming.protocol.replace(":", ""));

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
