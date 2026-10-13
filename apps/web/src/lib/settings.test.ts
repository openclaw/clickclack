import test from "node:test";
import assert from "node:assert/strict";
import { defaultSettingsWorkspace, isSettingsWorkspaceCurrent } from "./settings.ts";
import type { Workspace } from "./types";

const workspace = (id: string, overrides: Partial<Workspace> = {}): Workspace =>
  ({
    id,
    route_id: `route-${id}`,
    name: id,
    slug: id,
    icon_url: "",
    created_at: "2026-01-01T00:00:00Z",
    role: "member",
    ...overrides,
  }) as Workspace;

test("defaultSettingsWorkspace picks the workspace matched by id", () => {
  const workspaces = [workspace("W1"), workspace("W2"), workspace("W3")];
  assert.equal(defaultSettingsWorkspace(workspaces, "W2")?.id, "W2");
});

test("defaultSettingsWorkspace picks the workspace matched by route_id", () => {
  const workspaces = [workspace("W1"), workspace("W2", { route_id: "acme" }), workspace("W3")];
  assert.equal(defaultSettingsWorkspace(workspaces, "acme")?.id, "W2");
});

test("defaultSettingsWorkspace falls back to the first workspace when nothing matches", () => {
  const workspaces = [workspace("W1"), workspace("W2")];
  for (const id of ["missing", "", undefined, null]) {
    assert.equal(defaultSettingsWorkspace(workspaces, id)?.id, "W1");
  }
});

test("defaultSettingsWorkspace returns null for an empty list", () => {
  assert.equal(defaultSettingsWorkspace([], "W1"), null);
  assert.equal(defaultSettingsWorkspace([], undefined), null);
});

test("defaultSettingsWorkspace does not mutate the workspaces it was given", () => {
  const workspaces = [workspace("W1"), workspace("W2")];
  defaultSettingsWorkspace(workspaces, "W2");
  assert.deepEqual(
    workspaces.map((entry) => entry.id),
    ["W1", "W2"],
  );
});

test("isSettingsWorkspaceCurrent matches on either id", () => {
  const entry = workspace("W2", { route_id: "acme" });
  assert.equal(isSettingsWorkspaceCurrent(entry, "W2"), true);
  assert.equal(isSettingsWorkspaceCurrent(entry, "acme"), true);
});

test("isSettingsWorkspaceCurrent marks nothing current without an id to match", () => {
  const entry = workspace("W2");
  for (const id of ["W1", "", undefined, null]) {
    assert.equal(isSettingsWorkspaceCurrent(entry, id), false);
  }
});
