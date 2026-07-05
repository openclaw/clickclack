import { build } from "esbuild";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outfile = path.join(root, ".test", "contract.test.cjs");

await build({
  absWorkingDir: root,
  bundle: true,
  entryPoints: ["src/contract.test.ts"],
  format: "cjs",
  outfile,
  platform: "node",
  sourcemap: "inline",
  target: "node22",
});

const result = spawnSync(process.execPath, ["--test", outfile], { stdio: "inherit" });
process.exit(result.status ?? 1);
