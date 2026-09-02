import { expect, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";

// Workspace-scoped realtime keeps parallel tests from changing each other's rows and sidebar.
export async function createGeneralChannel(page: Page, label: string, isolatedUser = false) {
  const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
  const workspaceResponse = await page.request.post("/api/workspaces", {
    data: { name: `${label} ${suffix}` },
  });
  expect(workspaceResponse.ok()).toBe(true);
  const { workspace } = (await workspaceResponse.json()) as {
    workspace: { id: string; route_id: string };
  };
  if (isolatedUser) {
    const userID = execFileSync(
      "go",
      [
        "run",
        "./apps/api/cmd/clickclack",
        "admin",
        "user",
        "create",
        "--data",
        "./data/e2e",
        "--workspace",
        workspace.id,
        "--name",
        `${label} Tester`,
        "--email",
        `${label.toLowerCase().replaceAll(" ", "-")}-${suffix}@example.com`,
      ],
      { cwd: process.cwd(), encoding: "utf8" },
    ).trim();
    await page.setExtraHTTPHeaders({ "X-ClickClack-User": userID });
  }
  const channelResponse = await page.request.post(`/api/workspaces/${workspace.id}/channels`, {
    data: { name: "general", kind: "public" },
  });
  expect(channelResponse.ok()).toBe(true);
  const { channel } = (await channelResponse.json()) as {
    channel: { id: string; name: string; route_id: string };
  };
  return { workspace, channel, route: `/app/${workspace.route_id}/${channel.route_id}` };
}
