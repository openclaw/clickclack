import test from "node:test";
import assert from "node:assert/strict";
import { currentRole, isWorkspaceManager } from "./permissions.ts";
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

test("isWorkspaceManager is true only for owner and moderator roles", () => {
  assert.equal(isWorkspaceManager("owner"), true);
  assert.equal(isWorkspaceManager("moderator"), true);
  assert.equal(isWorkspaceManager("member"), false);
  assert.equal(isWorkspaceManager("guest"), false);
  assert.equal(isWorkspaceManager("bot"), false);
});

test("isWorkspaceManager treats missing roles as non-managers", () => {
  assert.equal(isWorkspaceManager(undefined), false);
  assert.equal(isWorkspaceManager(null), false);
});

test("currentRole matches a workspace by id or by route_id", () => {
  const workspaces = [
    workspace("W1", { role: "owner" }),
    workspace("W2", { route_id: "acme", role: "moderator" }),
  ];
  assert.equal(currentRole(workspaces, "W1"), "owner");
  assert.equal(currentRole(workspaces, "acme"), "moderator");
});

test("currentRole returns undefined when the id is blank or unmatched", () => {
  const workspaces = [workspace("W1", { role: "owner" })];
  assert.equal(currentRole(workspaces, undefined), undefined);
  assert.equal(currentRole(workspaces, null), undefined);
  assert.equal(currentRole(workspaces, ""), undefined);
  assert.equal(currentRole(workspaces, "missing"), undefined);
});

test("currentRole surfaces an undefined role on a matched workspace", () => {
  const workspaces = [workspace("W1", { role: undefined })];
  assert.equal(currentRole(workspaces, "W1"), undefined);
});
