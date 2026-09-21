import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repo = path.resolve(root, "../..");
const require = createRequire(import.meta.url);

test("Windows release config signs owned files and preserves preview configuration", async () => {
  const builderRequire = createRequire(require.resolve("electron-builder"));
  const appBuilderRequire = createRequire(builderRequire.resolve("app-builder-lib"));
  const { getConfig, validateConfiguration } = appBuilderRequire("./util/config/config");
  const { DebugLogger } = appBuilderRequire("builder-util");
  const { WinPackager } = appBuilderRequire("./winPackager");
  const preview = await getConfig(root, "electron-builder.yml", null);
  const release = await getConfig(root, "electron-builder.windows-release.yml", null);
  await validateConfiguration(release, new DebugLogger());
  assert.equal(release.forceCodeSigning, true);
  assert.notEqual(preview.forceCodeSigning, true);
  assert.equal(preview.win.azureSignOptions, undefined);
  assert.deepEqual(release.win.target, preview.win.target);
  assert.deepEqual(release.nsis, preview.nsis);
  assert.deepEqual(release.files, preview.files);
  assert.deepEqual(release.mac, preview.mac);
  assert.deepEqual(release.linux, preview.linux);
  // The verifier resolves this same default, checksummed NSIS toolset.
  assert.equal(release.toolsets?.nsis, undefined);
  assert.equal(release.nsis.customNsisBinary, undefined);
  assert.equal(release.win.azureSignOptions.publisherName, "OpenClaw Foundation");
  assert.equal(release.win.azureSignOptions.timestampDigest, "SHA256");
  for (const [filename, expected] of [
    ["ClickClack.exe", true],
    ["ClickClack-1.2.3-win-x64.exe", true],
    ["Uninstall ClickClack.exe", true],
    ["resources/elevate.exe", false],
    ["vendor.dll", false],
  ]) {
    assert.equal(
      WinPackager.prototype.shouldSignFile.call(
        { platformSpecificBuildOptions: release.win },
        filename,
      ),
      expected,
      filename,
    );
  }
});

test("release workflow gates Windows checksums and upload on signing verification", async () => {
  const workflow = await readFile(path.join(repo, ".github/workflows/release.yml"), "utf8");
  const windows = workflow.split("  desktop-windows:\n")[1].split("  desktop-linux:\n")[0];
  const linux = workflow.split("  desktop-linux:\n")[1].split("  verify-macos:\n")[0];
  const order = [
    "Require Windows signing identity",
    "Authenticate Windows signer with OIDC",
    "--config electron-builder.windows-release.yml",
    "Verify signed Windows release assets",
    "Write desktop checksums",
    "Upload desktop release candidates",
  ];
  let previous = -1;
  for (const marker of order) {
    const position = windows.indexOf(marker);
    assert.ok(position > previous, marker);
    previous = position;
  }
  assert.match(windows, /environment: release-signing/);
  assert.match(windows, /id-token: write/);
  assert.doesNotMatch(linux, /azure\/login|id-token|azureSignOptions|release-signing/);
  const preview = await readFile(path.join(repo, ".github/workflows/desktop.yml"), "utf8");
  assert.doesNotMatch(preview, /azure\/login|id-token|electron-builder\.windows-release\.yml/);
  assert.match(preview, /windows-signing\.test\.ps1 -UnsignedReleaseDirectory/);
});

test("Windows signature policy fixtures", { skip: process.platform !== "win32" }, () => {
  const env = Object.fromEntries(
    Object.entries(process.env).filter(([key]) => !key.startsWith("AZURE_")),
  );
  const result = spawnSync(
    "pwsh",
    ["-NoProfile", "-NonInteractive", "-File", path.join(root, "scripts/windows-signing.test.ps1")],
    { encoding: "utf8", env },
  );
  assert.equal(result.status, 0, result.error?.message ?? result.stdout + result.stderr);
});
