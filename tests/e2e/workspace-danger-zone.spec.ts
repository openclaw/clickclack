import { expect, test, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";

async function openOwnedWorkspaceSettings(page: Page) {
  const name = `Danger Zone ${randomUUID().replaceAll("-", "").slice(0, 10)}`;
  const response = await page.request.post("/api/workspaces", { data: { name } });
  expect(response.ok()).toBe(true);
  const { workspace } = (await response.json()) as { workspace: { route_id: string } };
  await page.goto(`/app/${workspace.route_id}/settings/overview`);
  await expect(page.getByRole("heading", { name: "Danger zone" })).toBeVisible();
  return name;
}

test("delete workspace names what to type before the button enables", async ({ page }) => {
  const name = await openOwnedWorkspaceSettings(page);
  await expect(page.getByText("Type the workspace name to confirm.")).toBeVisible();

  const confirm = page.getByRole("textbox", {
    name: "Type the workspace name to confirm deletion",
  });
  const remove = page.getByRole("button", { name: "Delete workspace" });
  await expect(remove).toBeDisabled();
  await confirm.fill(`${name} nope`);
  await expect(remove).toBeDisabled();
  await confirm.fill(name);
  await expect(remove).toBeEnabled();
});

test("transfer ownership says when no member is eligible", async ({ page }) => {
  await openOwnedWorkspaceSettings(page);
  const owner = page.getByRole("combobox", { name: "New workspace owner" });
  await expect(owner).toBeDisabled();
  await expect(owner).toHaveText("No eligible members");
  await expect(page.getByText("Add a human member who is not a guest first.")).toBeVisible();
});

test("transfer ownership keeps the empty message out of other states", async ({ page }) => {
  await page.route("**/api/workspaces/*/members*", (route) =>
    route.fulfill({ status: 500, json: { error: "members unavailable" } }),
  );
  await openOwnedWorkspaceSettings(page);
  await expect(page.locator(".ws-status.is-error")).not.toBeEmpty();
  const owner = page.getByRole("combobox", { name: "New workspace owner" });
  await expect(owner).not.toHaveText("No eligible members");
  await expect(page.getByText("Add a human member who is not a guest first.")).toHaveCount(0);

  await page.unroute("**/api/workspaces/*/members*");
  await page.route("**/api/workspaces/*/members*", (route) =>
    route.fulfill({
      json: {
        members: [{ role: "member", user: { id: "usr_ada", kind: "human", display_name: "Ada" } }],
      },
    }),
  );
  await page.reload();
  await expect(owner).toBeEnabled();
  await expect(owner).toHaveValue("usr_ada");
  await expect(owner).toHaveText("Ada");
  await expect(page.getByText("Add a human member who is not a guest first.")).toHaveCount(0);
});
