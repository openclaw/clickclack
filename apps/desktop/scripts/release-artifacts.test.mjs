import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import {
  expectedDesktopArtifactNames,
  normalizeReleaseVersion,
  writeDesktopChecksums,
} from "./release-artifacts.mjs";

test("release dry runs build current main without publication authority", () => {
  const workflow = readFileSync(
    new URL("../../../.github/workflows/release.yml", import.meta.url),
    "utf8",
  ).replaceAll("\r\n", "\n");
  const jobs = new Map(
    [
      ...workflow
        .split("\njobs:\n")[1]
        .matchAll(/^  ([\w-]+):\n([\s\S]*?)(?=^  [\w-]+:\n|$(?![\s\S]))/gm),
    ].map(([, name, body]) => [name, body]),
  );
  const dryRun = jobs.get("dry-run");
  assert.ok(dryRun);
  assert.match(dryRun, /if: >-\s+inputs\.dry_run &&/);
  assert.match(dryRun, /permissions:\s+contents: read/);
  assert.match(dryRun, /ref: \$\{\{ github\.sha \}\}/);
  assert.match(dryRun, /args: release --snapshot --clean --skip=publish/);
  assert.doesNotMatch(dryRun, /secrets\.|contents: write|gh release|upload-artifact/);
  for (const [name, body] of jobs) {
    if (name !== "dry-run") assert.match(body, /if: >-\s+!inputs\.dry_run &&/, name);
  }
});

test("normalizes semantic release tags", () => {
  assert.equal(normalizeReleaseVersion("v1.2.3"), "1.2.3");
  assert.equal(normalizeReleaseVersion("1.2.3-beta.1"), "1.2.3-beta.1");
  assert.throws(() => normalizeReleaseVersion("v1.2"), /semantic version/);
  assert.throws(() => normalizeReleaseVersion("v01.2.3"), /semantic version/);
});

test("describes every expected native installer", () => {
  assert.deepEqual(expectedDesktopArtifactNames("mac", "v1.2.3"), [
    "ClickClack-1.2.3-mac-arm64.dmg",
    "ClickClack-1.2.3-mac-arm64.zip",
    "ClickClack-1.2.3-mac-x64.dmg",
    "ClickClack-1.2.3-mac-x64.zip",
  ]);
  assert.deepEqual(expectedDesktopArtifactNames("win", "1.2.3"), [
    "ClickClack-1.2.3-win-x64.exe",
    "ClickClack-1.2.3-win-x64.zip",
  ]);
  assert.deepEqual(expectedDesktopArtifactNames("linux", "1.2.3"), [
    "ClickClack-1.2.3-linux-amd64.deb",
    "ClickClack-1.2.3-linux-x86_64.AppImage",
  ]);
});

test("writes a sorted SHA-256 manifest and rejects missing installers", (context) => {
  const directory = mkdtempSync(path.join(tmpdir(), "clickclack-desktop-release-"));
  context.after(() => rmSync(directory, { force: true, recursive: true }));
  const names = expectedDesktopArtifactNames("win", "1.2.3");
  for (const name of names) writeFileSync(path.join(directory, name), name);

  const output = writeDesktopChecksums(directory, "win", "1.2.3");
  const expected = [...names]
    .sort()
    .map((name) => `${createHash("sha256").update(name).digest("hex")}  ${name}`)
    .join("\n");
  assert.equal(readFileSync(output, "utf8"), `${expected}\n`);

  assert.throws(() => writeDesktopChecksums(directory, "mac", "1.2.3"), /Desktop release artifact/);
});
