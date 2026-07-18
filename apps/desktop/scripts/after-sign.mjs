import { execFile as execFileCallback } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFile = promisify(execFileCallback);

export const EXPECTED_IDENTITY = "Developer ID Application: OpenClaw Foundation (FWJYW4S8P8)";
export const EXPECTED_IDENTITY_QUALIFIER = "OpenClaw Foundation (FWJYW4S8P8)";
export const NOTARY_RETRY_DELAY_MS = 10 * 60 * 1000;

function commandFailure(error) {
  return [error?.message, error?.stdout, error?.stderr].filter(Boolean).join("\n");
}

export function isConnectTimeout(error) {
  return /connectTimeout/i.test(commandFailure(error));
}

export async function submitNotarizationWithRetry(submit, delay) {
  try {
    return await submit();
  } catch (error) {
    if (!isConnectTimeout(error)) {
      throw error;
    }
    console.warn("notarytool hit connectTimeout; waiting 10 minutes before one retry");
    await delay(NOTARY_RETRY_DELAY_MS);
    return submit();
  }
}

export function assertAcceptedNotarization(rawResult) {
  let result;
  try {
    result = JSON.parse(rawResult);
  } catch {
    throw new Error("notarytool did not return valid JSON");
  }
  if (result.status !== "Accepted") {
    throw new Error(`notarytool returned ${result.status || "no status"}, expected Accepted`);
  }
  if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(result.id || "")) {
    throw new Error("notarytool returned an invalid submission id");
  }
  return result;
}

async function run(command, args, options = {}) {
  return execFile(command, args, {
    encoding: "utf8",
    maxBuffer: 10 * 1024 * 1024,
    ...options,
  });
}

async function verifyApp(script, appPath, requireNotarized = false) {
  const args = requireNotarized ? ["--require-notarized", appPath] : [appPath];
  await run("bash", [script, ...args]);
}

export async function notarizeApp(appPath, options = {}) {
  const profile = options.profile || process.env.NOTARYTOOL_KEYCHAIN_PROFILE;
  if (!profile) {
    throw new Error("official macOS packaging requires NOTARYTOOL_KEYCHAIN_PROFILE");
  }

  const identity = options.identity || process.env.CSC_NAME;
  if (identity !== EXPECTED_IDENTITY_QUALIFIER) {
    throw new Error(`official macOS packaging requires ${EXPECTED_IDENTITY_QUALIFIER}`);
  }

  const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "verify-macos-app.sh");
  const workDirectory = await mkdtemp(path.join(os.tmpdir(), "clickclack-notary."));
  const submission = path.join(workDirectory, "ClickClack.zip");
  const delay =
    options.delay ||
    ((milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)));

  try {
    await verifyApp(script, appPath);
    await run("ditto", ["-c", "-k", "--sequesterRsrc", "--keepParent", appPath, submission]);

    const submit = async () => {
      const { stdout } = await run("xcrun", [
        "notarytool",
        "submit",
        submission,
        "--keychain-profile",
        profile,
        "--no-s3-acceleration",
        "--wait",
        "--output-format",
        "json",
      ]);
      return stdout;
    };
    const result = await submitNotarizationWithRetry(submit, delay);
    assertAcceptedNotarization(result);

    await run("xcrun", ["stapler", "staple", appPath]);
    await verifyApp(script, appPath, true);
  } finally {
    await rm(workDirectory, { recursive: true, force: true });
  }
}

export default async function afterSign(context) {
  if (
    context.electronPlatformName !== "darwin" ||
    process.env.CLICKCLACK_OFFICIAL_MACOS_RELEASE !== "1"
  ) {
    return;
  }

  const appName = `${context.packager.appInfo.productFilename}.app`;
  await notarizeApp(path.join(context.appOutDir, appName));
}
