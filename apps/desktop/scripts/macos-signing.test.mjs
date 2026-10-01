import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  EXPECTED_IDENTITY,
  EXPECTED_IDENTITY_QUALIFIER,
  NOTARY_RETRY_DELAY_MS,
  assertAcceptedNotarization,
  submitNotarizationWithRetry,
} from "./after-sign.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repo = path.resolve(root, "../..");
const accepted = JSON.stringify({
  id: "12345678-1234-1234-1234-123456789abc",
  status: "Accepted",
});

test("notarization retries one connectTimeout after exactly ten minutes", async () => {
  let attempts = 0;
  const delays = [];
  const result = await submitNotarizationWithRetry(
    async () => {
      attempts += 1;
      if (attempts === 1) {
        const error = new Error("notary transport failed");
        error.stderr = "connectTimeout";
        throw error;
      }
      return accepted;
    },
    async (milliseconds) => delays.push(milliseconds),
  );
  assert.equal(result, accepted);
  assert.equal(attempts, 2);
  assert.deepEqual(delays, [NOTARY_RETRY_DELAY_MS]);
});

test("notarization does not retry other failures", async () => {
  let attempts = 0;
  await assert.rejects(
    submitNotarizationWithRetry(
      async () => {
        attempts += 1;
        throw new Error("invalid credentials");
      },
      async () => assert.fail("unexpected delay"),
    ),
    /invalid credentials/,
  );
  assert.equal(attempts, 1);
});

test("notarization accepts only Apple's accepted response shape", () => {
  assert.equal(assertAcceptedNotarization(accepted).status, "Accepted");
  assert.throws(() => assertAcceptedNotarization("{}"), /no status/);
  assert.throws(
    () => assertAcceptedNotarization('{"id":"not-a-uuid","status":"Accepted"}'),
    /invalid submission id/,
  );
});

test(
  "notarization forwards a credential-keychain path as one argument",
  { skip: process.platform === "win32" },
  async () => {
    const scratch = await mkdtemp(path.join(os.tmpdir(), "clickclack-notary-test-"));
    try {
      const argsFile = path.join(scratch, "args.txt");
      await writeFile(
        path.join(scratch, "xcrun"),
        `#!/bin/sh\nprintf '%s\\n' "$@" > "$NOTARY_TEST_ARGS"\nprintf '%s\\n' '${accepted}'\n`,
        { mode: 0o755 },
      );
      const keychainPath = path.join(scratch, "release credentials.keychain-db");
      const moduleURL = new URL("./after-sign.mjs", import.meta.url).href;
      execFileSync(
        process.execPath,
        [
          "--input-type=module",
          "-e",
          `import { notarizeArchive } from ${JSON.stringify(moduleURL)}; await notarizeArchive("synthetic.zip");`,
        ],
        {
          env: {
            PATH: scratch,
            NOTARY_TEST_ARGS: argsFile,
            NOTARYTOOL_KEYCHAIN_PROFILE: "synthetic-profile",
            NOTARYTOOL_KEYCHAIN_PATH: keychainPath,
          },
        },
      );
      const args = (await readFile(argsFile, "utf8")).trim().split("\n");
      assert.equal(args[args.indexOf("--keychain") + 1], keychainPath);
      assert.equal(args[args.indexOf("--keychain-profile") + 1], "synthetic-profile");
    } finally {
      await rm(scratch, { recursive: true, force: true });
    }
  },
);

test("macOS release policy is fail-closed and Foundation-scoped", async () => {
  const [config, packageScript, verifier, workflow] = await Promise.all([
    readFile(path.join(root, "electron-builder.yml"), "utf8"),
    readFile(path.join(root, "scripts", "package-macos-release.sh"), "utf8"),
    readFile(path.join(root, "scripts", "verify-macos-app.sh"), "utf8"),
    readFile(path.join(repo, ".github", "workflows", "release.yml"), "utf8"),
  ]);

  assert.match(config, /afterSign: scripts\/after-sign\.mjs/);
  assert.match(config, /hardenedRuntime: true/);
  assert.match(config, /strictVerify: true/);
  assert.match(config, /notarize: false/);
  assert.match(verifier, new RegExp(RegExp.escape(EXPECTED_IDENTITY)));
  assert.match(packageScript, new RegExp(RegExp.escape(EXPECTED_IDENTITY_QUALIFIER)));
  assert.match(packageScript, /NOTARYTOOL_KEYCHAIN_PROFILE/);
  assert.match(packageScript, /tag -v/);
  assert.match(verifier, /codesign --verify --strict --deep/);
  assert.match(verifier, /chat\.clickclack\.desktop/);
  assert.match(verifier, /FWJYW4S8P8/);
  assert.match(workflow, /Verify signed macOS release assets/);
});
