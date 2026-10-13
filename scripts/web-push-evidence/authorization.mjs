// Production-path evidence that a queued web push is re-authorized before it
// is sent. It builds the server with the end-to-end tag (the only change that
// tag makes to push is accepting a loopback push service), runs it with
// development authentication off, and points every device at a fake push
// service on loopback.
//
// Each case holds all four delivery workers on the fake service, posts a real
// message so the recipient's push waits in the queue, withdraws one piece of
// the recipient's authority, and releases the workers. The recipient's device
// must receive nothing, except in the control case, where it receives one push.
// In the key-retired case the device's row is moved to a key the server does
// not sign with, the way a rotation leaves it; afterwards the push state must
// name the device stale, and a message posted with the workers free must not
// be queued for it at all.
//
// A last case is one browser shared by two accounts. Signed in by cookie as A,
// the device registers for A; the same cookie jar then signs in as B, and a
// registration that still names A, the way a tab left open on A asks after a
// renewal, must be refused with nothing written. B's messages must reach no
// device, and A's must still reach A's.
//
//   node scripts/web-push-evidence/authorization.mjs --out <dir>
//
// Writes summary.txt, server-webpush.log (the web push lines), and server.log
// (everything the server printed) to <dir>. Session tokens stay in memory; the
// run fails if a token or an endpoint path appears in the server's output.

import { execFileSync, spawn } from "node:child_process";
import { createHash, generateKeyPairSync, randomUUID } from "node:crypto";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

const WORKERS = 4;
const CASES = [
  "control",
  "session-revoked",
  "subscription-deleted",
  "membership-removed",
  "key-retired",
];
const SKIP_REASON = {
  "session-revoked": "the session that registered the device has ended",
  "subscription-deleted": "the device is no longer registered",
  "membership-removed": "the user can no longer read the message",
  "key-retired": "the device was registered under a retired key",
};

const outIndex = process.argv.indexOf("--out");
if (outIndex < 0 || !process.argv[outIndex + 1]) {
  console.error("usage: node scripts/web-push-evidence/authorization.mjs --out <dir>");
  process.exit(2);
}
const outDir = resolve(process.argv[outIndex + 1]);
mkdirSync(outDir, { recursive: true });
const repoRoot = resolve(import.meta.dirname, "../..");
const workDir = mkdtempSync(join(tmpdir(), "clickclack-push-evidence-"));
const dataDir = join(workDir, "data");
const binary = join(workDir, "clickclack");
const secrets = [];
const endpointPaths = [];
let server;
const deadline = setTimeout(() => fail("the run exceeded its five minute deadline"), 5 * 60_000);

function fail(message) {
  console.error(`FAIL: ${message}`);
  server?.kill("SIGKILL");
  process.exit(1);
}

function cli(...args) {
  const env = Object.fromEntries(
    Object.entries(process.env).filter(([name]) => !name.startsWith("CLICKCLACK_")),
  );
  return execFileSync(binary, args, { cwd: repoRoot, env, encoding: "utf8" }).trim();
}

// The fake push service. Endpoints under /hold/ wait while the gate is shut;
// every accepted delivery is counted under the case name in its path.
let gateOpen = false;
let releaseGate = () => {};
let gate = Promise.resolve();
let held = 0;
const delivered = new Map();
function shutGate() {
  gateOpen = false;
  held = 0;
  gate = new Promise((resolve) => {
    releaseGate = resolve;
  });
}
function openGate() {
  gateOpen = true;
  releaseGate();
}
const relay = createServer((request, response) => {
  request.resume();
  request.on("end", async () => {
    const [, kind, label] = (request.url ?? "").split("/");
    if (kind === "hold" && !gateOpen) {
      held += 1;
      await gate;
    }
    if (kind === "push") delivered.set(label, (delivered.get(label) ?? 0) + 1);
    response.writeHead(201);
    response.end();
  });
});
await new Promise((resolve) => relay.listen(0, "127.0.0.1", resolve));
const relayOrigin = `http://127.0.0.1:${relay.address().port}`;

console.log("building the server with the end-to-end tag");
execFileSync(
  "go",
  ["build", "-tags", "clickclack_e2e_unsafe_callbacks", "-o", binary, "./apps/api/cmd/clickclack"],
  { cwd: repoRoot, stdio: "inherit" },
);

const owner = cli(
  "admin",
  "bootstrap",
  "--data",
  dataDir,
  "--name",
  "Owner",
  "--email",
  "owner@evidence.test",
);
const vapid = generateVAPIDKeys();
const port = await freePort();
const origin = `http://127.0.0.1:${port}`;
const logLines = [];
server = spawn(binary, ["serve", "--addr", `127.0.0.1:${port}`, "--data", dataDir], {
  cwd: repoRoot,
  env: {
    ...Object.fromEntries(
      Object.entries(process.env).filter(([name]) => !name.startsWith("CLICKCLACK_")),
    ),
    CLICKCLACK_WEBPUSH_VAPID_PUBLIC_KEY: vapid.publicKey,
    CLICKCLACK_WEBPUSH_VAPID_PRIVATE_KEY: vapid.privateKey,
    CLICKCLACK_WEBPUSH_SUBJECT: "mailto:evidence@clickclack.test",
  },
});
for (const stream of [server.stdout, server.stderr]) {
  stream.setEncoding("utf8");
  let partial = "";
  stream.on("data", (chunk) => {
    const lines = (partial + chunk).split("\n");
    partial = lines.pop() ?? "";
    logLines.push(...lines);
  });
}
server.on("exit", (code) => {
  if (code !== null && code !== 0) fail(`the server exited with ${code}`);
});
await waitFor(async () => (await fetch(`${origin}/healthz`).catch(() => null))?.ok, "server start");

const ownerSession = await signIn("owner@evidence.test");

async function api(session, method, path, body) {
  const response = await fetch(`${origin}${path}`, {
    method,
    headers: { Authorization: `Bearer ${session}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) fail(`${method} ${path} answered ${response.status}: ${text}`);
  return text ? JSON.parse(text) : {};
}

async function signIn(email) {
  const token = cli("admin", "magic-link", "create", "--data", dataDir, "--email", email);
  const response = await fetch(`${origin}/api/auth/magic/consume`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
  });
  if (!response.ok) fail(`sign-in answered ${response.status}`);
  const session = (await response.json()).token;
  secrets.push(token, session);
  return session;
}

async function workspaceWithChannel(name) {
  const { workspace } = await api(ownerSession, "POST", "/api/workspaces", { name });
  const { channel } = await api(ownerSession, "POST", `/api/workspaces/${workspace.id}/channels`, {
    name: "general",
    kind: "public",
  });
  return { workspace: workspace.id, channel: channel.id };
}

async function member(workspace, name) {
  const email = `${name}-${randomUUID().slice(0, 8)}@evidence.test`;
  const id = cli(
    "admin",
    "user",
    "create",
    "--data",
    dataDir,
    "--workspace",
    workspace,
    "--name",
    name,
    "--email",
    email,
  );
  return { id, session: await signIn(email) };
}

// The RFC 8291 example subscription keys, a real point on P-256.
const subscriptionKeys = {
  p256dh:
    "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
  auth: "BTBZMqHH6r4Tts7J_aSIgg",
};

async function registerDevice(user, endpoint) {
  endpointPaths.push(new URL(endpoint).pathname);
  await api(user.session, "PUT", "/api/me/push/subscriptions", {
    user_id: user.id,
    endpoint,
    keys: subscriptionKeys,
    user_agent: "evidence device",
  });
}

// signInToJar signs in the way a browser does and returns the session cookie
// the server set, name and value, for a jar that the next sign-in replaces.
async function signInToJar(email) {
  const token = cli("admin", "magic-link", "create", "--data", dataDir, "--email", email);
  const response = await fetch(`${origin}/api/auth/magic/consume`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-ClickClack-CSRF": "1" },
    body: JSON.stringify({ token }),
  });
  if (!response.ok) fail(`sign-in answered ${response.status}`);
  const session = (await response.json()).token;
  secrets.push(token, session);
  const cookie = response.headers
    .getSetCookie()
    .map((header) => header.split(";")[0])
    .find((pair) => pair.endsWith(`=${session}`));
  if (!cookie) fail("sign-in set no session cookie");
  return cookie;
}

async function withCookie(cookie, method, path, body) {
  const response = await fetch(`${origin}${path}`, {
    method,
    headers: { Cookie: cookie, "Content-Type": "application/json", "X-ClickClack-CSRF": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  return { status: response.status, body: text ? JSON.parse(text) : {} };
}

// The blocker's four devices are what hold the workers on the fake service.
const blockerPlace = await workspaceWithChannel("Blocker");
const blocker = await member(blockerPlace.workspace, "blocker");
for (let index = 0; index < WORKERS; index += 1) {
  await registerDevice(blocker, `${relayOrigin}/hold/${index}/${randomUUID()}`);
}

const results = [];
let retired;
for (const name of CASES) {
  const place = await workspaceWithChannel(`Case ${name}`);
  const recipient = await member(place.workspace, "recipient");
  const endpoint = `${relayOrigin}/push/${name}/${randomUUID()}`;
  await registerDevice(recipient, endpoint);

  shutGate();
  await api(ownerSession, "POST", `/api/channels/${blockerPlace.channel}/messages`, {
    body: "hold the workers",
  });
  await waitFor(() => held >= WORKERS, "the workers to reach the held push service");
  await api(ownerSession, "POST", `/api/channels/${place.channel}/messages`, {
    body: `queued for ${name}`,
  });

  if (name === "session-revoked") {
    await api(recipient.session, "POST", "/api/auth/logout", {});
  } else if (name === "subscription-deleted") {
    await api(recipient.session, "DELETE", "/api/me/push/subscriptions", {
      user_id: recipient.id,
      endpoint,
    });
  } else if (name === "membership-removed") {
    // No endpoint removes a human member, so the harness makes the row change
    // an operator would. The delivery check reads the same row.
    for (const id of [place.workspace, recipient.id]) {
      if (!/^[A-Za-z0-9_]+$/.test(id)) fail(`unexpected id shape ${id}`);
    }
    execFileSync("sqlite3", [
      "-cmd",
      ".timeout 5000",
      join(dataDir, "clickclack.db"),
      `DELETE FROM workspace_members WHERE workspace_id = '${place.workspace}' AND user_id = '${recipient.id}';`,
    ]);
  } else if (name === "key-retired") {
    // A rotation needs a restart under a new key pair, so the harness moves
    // the device's row to another key instead, which is what a rotation
    // leaves behind. The delivery check reads the same row.
    if (!/^[A-Za-z0-9_]+$/.test(recipient.id)) fail(`unexpected id shape ${recipient.id}`);
    execFileSync("sqlite3", [
      "-cmd",
      ".timeout 5000",
      join(dataDir, "clickclack.db"),
      `UPDATE user_push_subscriptions SET vapid_key_id = '000000000000' WHERE user_id = '${recipient.id}';`,
    ]);
  }

  openGate();
  const outcome = `for user ${recipient.id}`;
  await waitFor(
    () =>
      logLines.some(
        (line) => line.includes(outcome) && /web push (delivered|delivery skipped)/.test(line),
      ),
    `the outcome for ${name}`,
  );
  const line = logLines.find(
    (entry) => entry.includes(outcome) && /web push (delivered|delivery skipped)/.test(entry),
  );
  results.push({
    name,
    recipient: recipient.id,
    delivered: delivered.get(name) ?? 0,
    line: line.replace(/^\S+ \S+ /, ""),
  });
  if (name === "key-retired") {
    const device = createHash("sha256").update(endpoint).digest("base64url");
    const state = await api(recipient.session, "GET", `/api/me/push?device=${device}`);
    const lines = () => logLines.filter((entry) => entry.includes(outcome)).length;
    const before = lines();
    await api(ownerSession, "POST", `/api/channels/${place.channel}/messages`, {
      body: "posted after the key was retired",
    });
    // Nothing is queued for a device under a retired key, so there is no
    // line to wait for; this settle only gives a push that was going to
    // leave the time one takes.
    await sleep(1_000);
    retired = {
      thisDevice: state.this_device,
      stale: state.this_device_stale,
      pushesAfter: (delivered.get(name) ?? 0) - results.at(-1).delivered,
      linesAfter: lines() - before,
    };
  }
}

// One browser, one cookie jar, two accounts. The workers are free now.
openGate();
const sharedA = await workspaceWithChannel("Shared browser A");
const sharedB = await workspaceWithChannel("Shared browser B");
const accountA = { email: `shared-a-${randomUUID().slice(0, 8)}@evidence.test` };
const accountB = { email: `shared-b-${randomUUID().slice(0, 8)}@evidence.test` };
for (const [account, place, name] of [
  [accountA, sharedA, "shared-a"],
  [accountB, sharedB, "shared-b"],
]) {
  account.id = cli(
    "admin",
    "user",
    "create",
    "--data",
    dataDir,
    "--workspace",
    place.workspace,
    "--name",
    name,
    "--email",
    account.email,
  );
}
const deviceA = `${relayOrigin}/push/shared-a/${randomUUID()}`;
const renewed = `${relayOrigin}/push/shared-renewed/${randomUUID()}`;
endpointPaths.push(new URL(deviceA).pathname, new URL(renewed).pathname);
let jar = await signInToJar(accountA.email);
const registeredA = await withCookie(jar, "PUT", "/api/me/push/subscriptions", {
  user_id: accountA.id,
  endpoint: deviceA,
  keys: subscriptionKeys,
  user_agent: "shared browser",
});
if (registeredA.status !== 200) fail(`A's own registration answered ${registeredA.status}`);
const jarA = jar;
jar = await signInToJar(accountB.email);
const me = await withCookie(jar, "GET", "/api/me");
const stale = await withCookie(jar, "PUT", "/api/me/push/subscriptions", {
  user_id: accountA.id,
  endpoint: renewed,
  keys: subscriptionKeys,
  user_agent: "shared browser",
});
const devicesB = (await withCookie(jar, "GET", "/api/me/push")).body.subscriptions.length;
const devicesA = (await withCookie(jarA, "GET", "/api/me/push")).body.subscriptions.length;
await api(ownerSession, "POST", `/api/channels/${sharedB.channel}/messages`, {
  body: "for the account that never turned push on",
});
await api(ownerSession, "POST", `/api/channels/${sharedA.channel}/messages`, {
  body: "for the account that did",
});
await waitFor(
  () => logLines.some((line) => line.includes(`web push delivered for user ${accountA.id}`)),
  "the delivery to A",
);
// B has no device, so nothing is queued for B; this settle only gives a push
// that was going to leave the time one takes.
await sleep(1_000);
const shared = {
  cookieOwner: me.body.user?.id === accountB.id ? "B" : `not B (${me.body.user?.id})`,
  staleStatus: stale.status,
  staleError: stale.body.error ?? "",
  devicesA,
  devicesB,
  pushesToRenewed: delivered.get("shared-renewed") ?? 0,
  pushesToA: delivered.get("shared-a") ?? 0,
};

clearTimeout(deadline);
server.kill("SIGTERM");
await new Promise((resolve) => server.once("exit", resolve));
relay.close();

const serverLog = logLines.join("\n") + "\n";
for (const secret of secrets) {
  if (serverLog.includes(secret)) fail("a session or sign-in token appeared in the server log");
}
if (serverLog.includes(relayOrigin) || endpointPaths.some((path) => serverLog.includes(path))) {
  fail("an endpoint appeared in the server log");
}

const problems = [];
for (const result of results) {
  const want = result.name === "control" ? 1 : 0;
  if (result.delivered !== want)
    problems.push(`${result.name}: ${result.delivered} pushes, want ${want}`);
  const reason = SKIP_REASON[result.name];
  if (reason && !result.line.endsWith(reason))
    problems.push(`${result.name}: logged "${result.line}"`);
  if (!reason && !result.line.includes("web push delivered"))
    problems.push(`control: logged "${result.line}"`);
}

if (!retired?.thisDevice || !retired?.stale)
  problems.push(`key-retired: the push state read ${JSON.stringify(retired)}`);
if (retired?.pushesAfter !== 0 || retired?.linesAfter !== 0)
  problems.push(`key-retired: a later message reached the device: ${JSON.stringify(retired)}`);
if (shared.cookieOwner !== "B") problems.push(`shared: the jar was ${shared.cookieOwner}`);
if (shared.staleStatus !== 409)
  problems.push(`shared: a registration naming A under B's cookie answered ${shared.staleStatus}`);
if (shared.devicesB !== 0) problems.push(`shared: B has ${shared.devicesB} devices, want 0`);
if (shared.devicesA !== 1) problems.push(`shared: A has ${shared.devicesA} devices, want 1`);
if (shared.pushesToRenewed !== 0)
  problems.push(`shared: ${shared.pushesToRenewed} pushes to the renewed endpoint, want 0`);
if (shared.pushesToA !== 1) problems.push(`shared: ${shared.pushesToA} pushes to A, want 1`);

const summary = [
  "Web push send-time authorization, production build path",
  `binary: go build -tags clickclack_e2e_unsafe_callbacks, serve without --dev-bootstrap`,
  `head: ${execFileSync("git", ["rev-parse", "--short", "HEAD"], { cwd: repoRoot, encoding: "utf8" }).trim()}`,
  `workers held on the fake push service before each case's message was posted: ${WORKERS}`,
  "",
  "case                   pushes to the recipient's device   server log line for the recipient",
  ...results.map(
    (result) => `${result.name.padEnd(22)} ${String(result.delivered).padEnd(34)} ${result.line}`,
  ),
  "",
  "Key retired: after the case above, with the workers free",
  `GET /api/me/push naming the device, this_device / this_device_stale: ${retired?.thisDevice} / ${retired?.stale}`,
  `a message posted after the retirement, pushes / log lines for the recipient: ${retired?.pushesAfter} / ${retired?.linesAfter}`,
  "",
  "Shared browser: one cookie jar, signed in as A, then as B",
  `A registers its device by cookie, naming A:              200`,
  `the jar's session after B signs in belongs to:          ${shared.cookieOwner}`,
  `registration naming A under B's cookie answered:        ${shared.staleStatus} ${shared.staleError}`,
  `devices listed for B / for A:                           ${shared.devicesB} / ${shared.devicesA}`,
  `pushes to the endpoint the stale registration named:    ${shared.pushesToRenewed} (a message to B was posted)`,
  `pushes to A's device:                                   ${shared.pushesToA} (a message to A was posted)`,
  "",
  problems.length === 0 ? "RESULT: PASS" : `RESULT: FAIL\n${problems.join("\n")}`,
  "",
].join("\n");
writeFileSync(join(outDir, "summary.txt"), summary);
writeFileSync(join(outDir, "server.log"), serverLog);
writeFileSync(
  join(outDir, "server-webpush.log"),
  logLines.filter((line) => line.includes("web push")).join("\n") + "\n",
);
rmSync(workDir, { recursive: true, force: true });
process.stdout.write(summary);
process.exit(problems.length === 0 ? 0 : 1);

async function waitFor(check, what) {
  const until = Date.now() + 20_000;
  while (Date.now() < until) {
    if (await check()) return;
    await sleep(50);
  }
  fail(`timed out waiting for ${what}`);
}

async function freePort() {
  const probe = createServer();
  await new Promise((resolve) => probe.listen(0, "127.0.0.1", resolve));
  const { port } = probe.address();
  await new Promise((resolve) => probe.close(resolve));
  return port;
}

function generateVAPIDKeys() {
  const pair = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
  const publicJWK = pair.publicKey.export({ format: "jwk" });
  const privateJWK = pair.privateKey.export({ format: "jwk" });
  const point = Buffer.concat([
    Buffer.from([4]),
    Buffer.from(publicJWK.x, "base64url"),
    Buffer.from(publicJWK.y, "base64url"),
  ]);
  return { publicKey: point.toString("base64url"), privateKey: privateJWK.d };
}
