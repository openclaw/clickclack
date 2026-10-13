import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { EXPECTED_IDENTITY, notarizeArchive } from "./after-sign.mjs";
import { normalizeReleaseVersion } from "./release-artifacts.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const [tag, destination] = process.argv.slice(2);
const version = normalizeReleaseVersion(tag);
const output = path.resolve(destination);
const run = (command, args, options = {}) =>
  String(
    execFileSync(command, args, {
      cwd: repo,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "inherit"],
      ...options,
    }) ?? "",
  ).trim();
if (process.platform !== "darwin" || !tag.startsWith("v"))
  throw new Error("A signed macOS release tag is required");
const commit = run("git", ["rev-parse", `${tag}^{commit}`]);
if (run("git", ["rev-parse", "HEAD"]) !== commit || run("git", ["status", "--porcelain"])) {
  throw new Error("macOS server packaging requires the clean release tag");
}
run("git", ["tag", "-v", tag]);
const date = run("git", ["show", "-s", "--format=%cI", commit]);
const files = run("git", ["ls-files"])
  .split("\n")
  .filter(
    (file) =>
      [
        "LICENSE",
        "README.md",
        "SPEC.md",
        "CHANGELOG.md",
        "docs/proof/switchboard-after-chat-light.png",
      ].includes(file) ||
      (file.startsWith("docs/") && file.endsWith(".md")) ||
      file.startsWith("docs/assets/"),
  );
const scratch = mkdtempSync(path.join(os.tmpdir(), "clickclack-server-release."));
const artifacts = [];
try {
  run("pnpm", ["build"], {
    stdio: "inherit",
    env: { ...process.env, CLICKCLACK_WEB_VERSION: commit.slice(0, 12) },
  });
  for (const [arch, label] of [
    ["amd64", "amd64"],
    ["arm64", "aarch64"],
  ]) {
    const stage = path.join(scratch, arch);
    mkdirSync(stage);
    for (const file of files) {
      mkdirSync(path.dirname(path.join(stage, file)), { recursive: true });
      cpSync(path.join(repo, file), path.join(stage, file));
    }
    const binary = path.join(stage, "clickclack");
    run(
      "go",
      [
        "build",
        "-p",
        "2",
        "-trimpath",
        "-ldflags",
        `-s -w -X main.version=${version} -X main.commit=${commit.slice(0, 7)} -X main.date=${date}`,
        "-o",
        binary,
        "./apps/api/cmd/clickclack",
      ],
      {
        env: { ...process.env, CGO_ENABLED: "0", GOOS: "darwin", GOARCH: arch },
      },
    );
    run("codesign", [
      "--force",
      "--sign",
      EXPECTED_IDENTITY,
      "--timestamp",
      "--options",
      "runtime",
      "--identifier",
      "chat.clickclack.server",
      binary,
    ]);
    const submission = path.join(scratch, `${arch}.zip`);
    run("ditto", ["-c", "-k", stage, submission]);
    const result = await notarizeArchive(submission);
    run("codesign", [
      "--verify",
      "--strict",
      "--check-notarization",
      "-R=notarized",
      "--verbose=2",
      binary,
    ]);
    const name = `clickclack_${version}_darwin_${label}.tar.gz`;
    run("tar", ["-czf", path.join(output, name), "-C", stage, "."]);
    artifacts.push({
      name,
      architecture: arch,
      sha256: createHash("sha256")
        .update(readFileSync(path.join(output, name)))
        .digest("hex"),
      notarization: { id: result.id, status: result.status },
    });
  }
  const proofName = `ClickClack-${version}-mac-server-notarization.json`;
  writeFileSync(
    path.join(output, proofName),
    `${JSON.stringify({ version, artifacts }, null, 2)}\n`,
  );
  const checksums = artifacts.map(({ name, sha256 }) => `${sha256}  ${name}`);
  checksums.push(
    `${createHash("sha256")
      .update(readFileSync(path.join(output, proofName)))
      .digest("hex")}  ${proofName}`,
  );
  writeFileSync(
    path.join(output, `ClickClack-${version}-mac-server-SHA256SUMS.txt`),
    `${checksums.sort().join("\n")}\n`,
  );
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
