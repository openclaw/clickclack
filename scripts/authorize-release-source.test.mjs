import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { authorizeReleaseSource, checkoutReleaseSource } from "./authorize-release-source.mjs";

test("only trusted signed tags from reviewed main history authorize release code", () => {
  const repository = mkdtempSync(path.join(os.tmpdir(), "clickclack-release-policy-"));
  const emptyConfig = path.join(repository, "empty-gitconfig");
  writeFileSync(emptyConfig, "");
  const env = { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: emptyConfig };
  const run = (command, args) =>
    execFileSync(command, args, {
      cwd: repository,
      env,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  const git = (...args) => run("git", args);
  try {
    git("init", "--initial-branch=main");
    git("config", "user.name", "Release policy fixture");
    git("config", "user.email", "release@example.invalid");
    git("config", "commit.gpgsign", "false");
    git("config", "gpg.format", "ssh");
    const key = path.join(repository, "signing-key");
    run("ssh-keygen", ["-q", "-t", "ed25519", "-N", "", "-f", key]);
    git("config", "user.signingkey", key);
    const signers = path.join(repository, "allowed-signers");
    writeFileSync(signers, `release@example.invalid ${readFileSync(`${key}.pub`, "utf8")}`);
    git("commit", "--allow-empty", "-m", "Reviewed source");
    const reviewed = git("rev-parse", "HEAD");
    git("tag", "-s", "v1.2.3", "-m", "Approved release");
    const authorized = authorizeReleaseSource(repository, "v1.2.3", reviewed, signers);
    assert.equal(authorized.commit, reviewed);
    assert.equal(authorized.tag_object, git("rev-parse", "refs/tags/v1.2.3"));
    assert.equal(authorized.version, "1.2.3");

    git("update-ref", "refs/tags/v9.9.9", authorized.tag_object);
    assert.throws(
      () => authorizeReleaseSource(repository, "v9.9.9", reviewed, signers),
      /name the requested release/,
    );

    git("branch", "v1.2.4");
    assert.throws(() => authorizeReleaseSource(repository, "v1.2.4", reviewed, signers));
    git("tag", "v1.2.5");
    assert.throws(
      () => authorizeReleaseSource(repository, "v1.2.5", reviewed, signers),
      /annotated/,
    );
    git("tag", "-a", "v1.2.6", "-m", "Unsigned tag");
    assert.throws(() => authorizeReleaseSource(repository, "v1.2.6", reviewed, signers));

    const stranger = path.join(repository, "unapproved-key");
    run("ssh-keygen", ["-q", "-t", "ed25519", "-N", "", "-f", stranger]);
    git("-c", `user.signingkey=${stranger}`, "tag", "-s", "v1.2.7", "-m", "Wrong signer");
    assert.throws(() => authorizeReleaseSource(repository, "v1.2.7", reviewed, signers));
    const revoked = path.join(repository, "empty-signers");
    writeFileSync(revoked, "");
    assert.throws(() => authorizeReleaseSource(repository, "v1.2.3", reviewed, revoked));

    git("checkout", "-b", "unreviewed");
    git("commit", "--allow-empty", "-m", "Not reviewed on main");
    git("tag", "-s", "v1.2.8", "-m", "Unreviewed source");
    assert.throws(() => authorizeReleaseSource(repository, "v1.2.8", reviewed, signers));
    for (const tag of ["main", "v1.2.3~1", "--help", "1.2.3"]) {
      assert.throws(
        () => authorizeReleaseSource(repository, tag, reviewed, signers),
        /semantic version/,
      );
    }
    assert.throws(
      () => authorizeReleaseSource(repository, "v1.2.3", "main", signers),
      /full commit SHA/,
    );
    const beforeCheckout = git("rev-parse", "HEAD");
    for (const expected of [
      { ...authorized, commit: beforeCheckout },
      { ...authorized, tag_object: "0".repeat(40) },
      {},
    ]) {
      assert.throws(
        () => checkoutReleaseSource(repository, "v1.2.3", reviewed, expected, signers),
        /changed after authorization/,
      );
      assert.equal(git("rev-parse", "HEAD"), beforeCheckout);
    }
    checkoutReleaseSource(repository, "v1.2.3", reviewed, authorized, signers);
    assert.equal(git("rev-parse", "HEAD"), reviewed);
  } finally {
    rmSync(repository, { recursive: true, force: true });
  }
});
