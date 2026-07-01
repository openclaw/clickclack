import { cpSync, mkdirSync, mkdtempSync, rmSync, renameSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const root = process.cwd();
const tempRoot = mkdtempSync(join(tmpdir(), "clickclack-go-test-"));
const worktree = join(tempRoot, "repo");

try {
  mkdirSync(worktree, { recursive: true });
  cpSync(resolve(root, "go.mod"), join(worktree, "go.mod"));
  cpSync(resolve(root, "go.sum"), join(worktree, "go.sum"));
  cpSync(resolve(root, "apps/api"), join(worktree, "apps/api"), { recursive: true });

  const embeddedDist = join(worktree, "apps/api/internal/webassets/dist");
  const stagedDist = join(tempRoot, "dist");
  cpSync(resolve(root, "apps/web/dist"), stagedDist, { recursive: true, dereference: true });
  rmSync(embeddedDist, { recursive: true, force: true });
  renameSync(stagedDist, embeddedDist);

  const result = spawnSync("go", ["test", "./..."], {
    cwd: worktree,
    env: process.env,
    stdio: "inherit",
  });
  if (result.error) {
    console.error(`failed to run go test: ${result.error.message}`);
    process.exitCode = 1;
  } else {
    process.exitCode = result.status ?? 1;
  }
} finally {
  rmSync(tempRoot, { recursive: true, force: true });
}
