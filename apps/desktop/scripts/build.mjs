import { build } from "esbuild";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

await build({
  absWorkingDir: root,
  bundle: true,
  entryPoints: {
    main: "src/main.ts",
    "app-preload": "src/app-preload.ts",
    "settings-preload": "src/settings-preload.ts",
  },
  external: ["electron"],
  format: "cjs",
  outdir: "dist",
  outExtension: { ".js": ".cjs" },
  platform: "node",
  sourcemap: true,
  target: "node22",
});

await build({
  absWorkingDir: root,
  bundle: true,
  entryPoints: ["src/settings.ts"],
  format: "iife",
  outfile: "dist/settings.js",
  platform: "browser",
  sourcemap: true,
  target: "chrome136",
});
