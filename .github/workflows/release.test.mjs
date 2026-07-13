import assert from "node:assert/strict";
import {
  chmod,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  realpath,
  rm,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const repositoryRoot = path.resolve(path.dirname(new URL(import.meta.url).pathname), "../..");
const releaseWorkflowPath = path.join(repositoryRoot, ".github/workflows/release.yml");
const goreleaserPath = path.join(repositoryRoot, ".goreleaser.yml");
const dockerfilePath = path.join(repositoryRoot, "Dockerfile");
const cloudflareDockerfilePath = path.join(repositoryRoot, "Dockerfile.cloudflare");
const wranglerPath = path.join(repositoryRoot, "wrangler.jsonc");
const dockerMetadataPath = path.join(repositoryRoot, "scripts/docker-build-metadata.sh");
const dockerBuildPath = path.join(repositoryRoot, "scripts/docker-build.sh");
const ciWorkflowPath = path.join(repositoryRoot, ".github/workflows/ci.yml");
const fakecoComposePath = path.join(repositoryRoot, "deploy/fakeco/compose.yaml");
const fakecoComposeWrapperPath = path.join(repositoryRoot, "deploy/fakeco/compose.sh");
const dockerignorePath = path.join(repositoryRoot, ".dockerignore");

function runGit(cwd, args, env = process.env) {
  const result = spawnSync("git", args, { cwd, encoding: "utf8", env });
  assert.equal(result.status, 0, result.stderr);
  return result.stdout.trim();
}

async function createMetadataRepository(t, prefix) {
  const temporary = await mkdtemp(path.join(os.tmpdir(), prefix));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  await mkdir(path.join(temporary, "scripts"), { recursive: true });
  await mkdir(path.join(temporary, "deploy/fakeco"), { recursive: true });
  await copyFile(dockerMetadataPath, path.join(temporary, "scripts/docker-build-metadata.sh"));
  await copyFile(dockerBuildPath, path.join(temporary, "scripts/docker-build.sh"));
  await copyFile(fakecoComposeWrapperPath, path.join(temporary, "deploy/fakeco/compose.sh"));
  await copyFile(fakecoComposePath, path.join(temporary, "deploy/fakeco/compose.yaml"));
  await chmod(path.join(temporary, "scripts/docker-build-metadata.sh"), 0o755);
  await chmod(path.join(temporary, "scripts/docker-build.sh"), 0o755);
  await chmod(path.join(temporary, "deploy/fakeco/compose.sh"), 0o755);
  await writeFile(path.join(temporary, "tracked.txt"), "release\n");
  runGit(temporary, ["init", "-q"]);
  runGit(temporary, ["config", "user.name", "ClickClack Test"]);
  runGit(temporary, ["config", "user.email", "test@clickclack.invalid"]);
  runGit(temporary, ["add", "."]);
  const buildDate = "2026-07-13T00:00:00+00:00";
  runGit(temporary, ["commit", "-q", "-m", "test"], {
    ...process.env,
    GIT_AUTHOR_DATE: buildDate,
    GIT_COMMITTER_DATE: buildDate,
  });
  return temporary;
}

test("release verifies the tag before every build and checks out the verified commit", async () => {
  const workflow = await readFile(releaseWorkflowPath, "utf8");
  const verifyStart = workflow.indexOf("verify-tag:");
  const serverStart = workflow.indexOf("\n  release:");
  const desktopStart = workflow.indexOf("\n  desktop:");
  assert.ok(verifyStart >= 0 && verifyStart < serverStart && verifyStart < desktopStart);
  assert.match(workflow, /git merge-base --is-ancestor "\$commit" refs\/remotes\/origin\/main/u);
  assert.match(workflow, /refs\/tags\/\$RELEASE_TAG\^\{commit\}/u);
  assert.match(workflow, /Manual releases must run from protected main/u);
  assert.equal(
    (workflow.match(/ref: \$\{\{ needs\.verify-tag\.outputs\.commit \}\}/gu) ?? []).length,
    2,
  );
  assert.match(workflow, /needs: verify-tag/u);
  assert.ok(
    workflow.indexOf("Verify tag is reachable from origin/main") <
      workflow.indexOf("Refuse to mutate a published release"),
  );
  assert.ok(workflow.indexOf("verify-tag:") < workflow.indexOf("Set up Go"));
  assert.ok(workflow.indexOf("verify-tag:") < workflow.indexOf("Set up pnpm"));
});

test("published releases are immutable and draft retries compare bytes", async () => {
  const [workflow, goreleaser] = await Promise.all([
    readFile(releaseWorkflowPath, "utf8"),
    readFile(goreleaserPath, "utf8"),
  ]);
  assert.match(workflow, /release --clean --skip=publish/u);
  assert.match(workflow, /\.draft == true/u);
  assert.match(workflow, /already published and immutable/u);
  assert.match(workflow, /cmp -s "\$candidate" "\$existing"/u);
  assert.match(
    workflow,
    /cmp -s server-release\/CHANGELOG\.md "\$RUNNER_TEMP\/existing-release-notes\.md"/u,
  );
  assert.match(workflow, /exists with different bytes; refusing replacement/u);
  assert.match(workflow, /Draft release asset set differs from the verified candidate manifest/u);
  assert.match(workflow, /jq -r '\.assets\[\]\.name'/u);
  assert.match(workflow, /comm -13 "\$expected_assets" "\$actual_assets"/u);
  assert.doesNotMatch(workflow, /--clobber/u);
  assert.match(goreleaser, /replace_existing_artifacts: false/u);
  assert.match(goreleaser, /mode: keep-existing/u);
  assert.match(goreleaser, /mod_timestamp: "\{\{ \.CommitTimestamp \}\}"/u);
  assert.match(goreleaser, /-X main\.date=\{\{ \.CommitDate \}\}/u);
  assert.doesNotMatch(goreleaser, /-X main\.date=\{\{ \.Date \}\}/u);
  assert.doesNotMatch(goreleaser, /replace_existing_artifacts: true|mode: replace/u);
});

test("production Docker builds require and verify linker metadata", async () => {
  const [dockerfile, cloudflareDockerfile] = await Promise.all([
    readFile(dockerfilePath, "utf8"),
    readFile(cloudflareDockerfilePath, "utf8"),
  ]);
  for (const source of [dockerfile, cloudflareDockerfile]) {
    assert.match(source, /-X main\.version=\$CLICKCLACK_VERSION/u);
    assert.match(source, /-X main\.commit=\$CLICKCLACK_COMMIT/u);
    assert.match(source, /-X main\.date=\$CLICKCLACK_BUILD_DATE/u);
    assert.match(
      source,
      /clickclack \$CLICKCLACK_VERSION \(\$CLICKCLACK_COMMIT, \$CLICKCLACK_BUILD_DATE\)/u,
    );
  }
  assert.match(dockerfile, /org\.opencontainers\.image\.version/u);
  assert.match(dockerfile, /org\.opencontainers\.image\.revision/u);
  assert.match(dockerfile, /org\.opencontainers\.image\.created/u);
  assert.match(cloudflareDockerfile, /COPY \.pnpm-store\/clickclack-cloudflare-build\.env/u);
});

test("repository Docker callsites derive and forward complete metadata", async (t) => {
  const [
    ciWorkflow,
    compose,
    composeWrapper,
    dockerignore,
    installDocs,
    deploymentDocs,
    fakecoDocs,
    readme,
  ] = await Promise.all([
    readFile(ciWorkflowPath, "utf8"),
    readFile(fakecoComposePath, "utf8"),
    readFile(fakecoComposeWrapperPath, "utf8"),
    readFile(dockerignorePath, "utf8"),
    readFile(path.join(repositoryRoot, "docs/install.md"), "utf8"),
    readFile(path.join(repositoryRoot, "docs/deployment.md"), "utf8"),
    readFile(path.join(repositoryRoot, "docs/fakeco.md"), "utf8"),
    readFile(path.join(repositoryRoot, "README.md"), "utf8"),
  ]);

  assert.match(
    ciWorkflow,
    /CLICKCLACK_BUILD_FLAVOR=ci scripts\/docker-build\.sh -t clickclack:ci \./u,
  );
  for (const variable of [
    "CLICKCLACK_VERSION",
    "CLICKCLACK_COMMIT",
    "CLICKCLACK_BUILD_DATE",
    "CLICKCLACK_WEB_VERSION",
  ]) {
    assert.ok(compose.includes(`${variable}: ` + "${" + `${variable}:?`));
  }
  assert.match(composeWrapper, /CLICKCLACK_BUILD_FLAVOR:-fakeco/u);
  assert.doesNotMatch(fakecoDocs, /docker compose/u);
  assert.match(fakecoDocs, /\.\/compose\.sh build/u);
  for (const source of [installDocs, deploymentDocs, readme]) {
    assert.match(source, /scripts\/docker-build\.sh -t clickclack \./u);
    assert.doesNotMatch(source, /^docker build /mu);
  }
  assert.match(dockerignore, /^\.pnpm-store\/\*$/mu);
  assert.match(dockerignore, /^!\.pnpm-store\/clickclack-cloudflare-build\.env$/mu);
  assert.ok(
    dockerignore.indexOf(".pnpm-store/*") <
      dockerignore.indexOf("!.pnpm-store/clickclack-cloudflare-build.env"),
  );

  const temporary = await createMetadataRepository(t, "clickclack-docker-metadata-");
  const fakeDockerDirectory = await mkdtemp(path.join(os.tmpdir(), "clickclack-fake-docker-"));
  t.after(() => rm(fakeDockerDirectory, { recursive: true, force: true }));
  const argsPath = path.join(fakeDockerDirectory, "docker-args.txt");
  await writeFile(
    path.join(fakeDockerDirectory, "docker"),
    '#!/usr/bin/env bash\nprintf "%s\\n" "$@" >"$DOCKER_ARGS_FILE"\n',
  );
  await chmod(path.join(fakeDockerDirectory, "docker"), 0o755);

  const cleanEnvironment = {
    ...process.env,
    CLICKCLACK_BUILD_FLAVOR: "ci",
    DOCKER_ARGS_FILE: argsPath,
    PATH: `${fakeDockerDirectory}:${process.env.PATH}`,
  };
  delete cleanEnvironment.CLICKCLACK_VERSION;
  delete cleanEnvironment.CLICKCLACK_COMMIT;
  delete cleanEnvironment.CLICKCLACK_BUILD_DATE;
  delete cleanEnvironment.CLICKCLACK_WEB_VERSION;

  const built = spawnSync("scripts/docker-build.sh", ["-t", "clickclack:ci", "."], {
    cwd: temporary,
    encoding: "utf8",
    env: cleanEnvironment,
  });
  assert.equal(built.status, 0, built.stderr);

  const head = runGit(temporary, ["rev-parse", "HEAD"]);
  const committedDate = runGit(temporary, ["show", "-s", "--format=%cI", "HEAD"]);
  assert.deepEqual((await readFile(argsPath, "utf8")).trim().split("\n"), [
    "build",
    "--build-arg",
    `CLICKCLACK_VERSION=0.0.0-ci.${head.slice(0, 12)}`,
    "--build-arg",
    `CLICKCLACK_COMMIT=${head}`,
    "--build-arg",
    `CLICKCLACK_BUILD_DATE=${committedDate}`,
    "--build-arg",
    `CLICKCLACK_WEB_VERSION=${head}`,
    "-t",
    "clickclack:ci",
    ".",
  ]);

  for (const override of [
    ["--build-arg", "CLICKCLACK_COMMIT=attacker"],
    ["--build-arg=CLICKCLACK_VERSION=attacker"],
  ]) {
    const rejected = spawnSync("scripts/docker-build.sh", [...override, "."], {
      cwd: temporary,
      encoding: "utf8",
      env: cleanEnvironment,
    });
    assert.equal(rejected.status, 64);
    assert.match(rejected.stderr, /cannot be overridden/u);
  }

  const composeArgsPath = path.join(fakeDockerDirectory, "compose-args.txt");
  await writeFile(
    path.join(fakeDockerDirectory, "docker"),
    "#!/usr/bin/env bash\n" +
      'printf "VERSION=%s\\nCOMMIT=%s\\nDATE=%s\\nWEB=%s\\n" \\\n' +
      '  "$CLICKCLACK_VERSION" "$CLICKCLACK_COMMIT" "$CLICKCLACK_BUILD_DATE" \\\n' +
      '  "$CLICKCLACK_WEB_VERSION" >"$DOCKER_ARGS_FILE"\n' +
      'printf "%s\\n" "$@" >>"$DOCKER_ARGS_FILE"\n',
  );
  const fakecoEnvironment = { ...cleanEnvironment, DOCKER_ARGS_FILE: composeArgsPath };
  delete fakecoEnvironment.CLICKCLACK_BUILD_FLAVOR;
  const composed = spawnSync("deploy/fakeco/compose.sh", ["build"], {
    cwd: temporary,
    encoding: "utf8",
    env: fakecoEnvironment,
  });
  assert.equal(composed.status, 0, composed.stderr);
  const canonicalTemporary = await realpath(temporary);
  assert.deepEqual((await readFile(composeArgsPath, "utf8")).trim().split("\n"), [
    `VERSION=0.0.0-fakeco.${head.slice(0, 12)}`,
    `COMMIT=${head}`,
    `DATE=${committedDate}`,
    `WEB=${head}`,
    "compose",
    "--project-directory",
    path.join(canonicalTemporary, "deploy/fakeco"),
    "--file",
    path.join(canonicalTemporary, "deploy/fakeco/compose.yaml"),
    "build",
  ]);

  await writeFile(path.join(temporary, "tracked.txt"), "dirty\n");
  const dirty = spawnSync("scripts/docker-build.sh", ["-t", "clickclack:ci", "."], {
    cwd: temporary,
    encoding: "utf8",
    env: cleanEnvironment,
  });
  assert.notEqual(dirty.status, 0);
  assert.match(dirty.stderr, /require a clean Git worktree/u);

  const inspected = spawnSync("deploy/fakeco/compose.sh", ["ps"], {
    cwd: temporary,
    encoding: "utf8",
    env: fakecoEnvironment,
  });
  assert.equal(inspected.status, 0, inspected.stderr);
  assert.match(await readFile(composeArgsPath, "utf8"), /\nps\n?$/u);
});

test("Cloudflare disables workers.dev and derives metadata from a clean commit", async (t) => {
  const wrangler = JSON.parse(await readFile(wranglerPath, "utf8"));
  assert.equal(wrangler.workers_dev, false);
  assert.match(wrangler.build.command, /CLICKCLACK_BUILD_FLAVOR=cloudflare/u);
  assert.match(wrangler.build.command, /source scripts\/docker-build-metadata\.sh/u);

  const temporary = await createMetadataRepository(t, "clickclack-cloudflare-metadata-");

  const generated = spawnSync("sh", ["-c", wrangler.build.command], {
    cwd: temporary,
    encoding: "utf8",
  });
  assert.equal(generated.status, 0, generated.stderr);
  const head = runGit(temporary, ["rev-parse", "HEAD"]);
  const committedDate = runGit(temporary, ["show", "-s", "--format=%cI", "HEAD"]);
  assert.equal(
    await readFile(path.join(temporary, ".pnpm-store/clickclack-cloudflare-build.env"), "utf8"),
    `CLICKCLACK_VERSION=0.0.0-cloudflare.${head.slice(0, 12)}\n` +
      `CLICKCLACK_COMMIT=${head}\n` +
      `CLICKCLACK_BUILD_DATE=${committedDate}\n`,
  );

  await writeFile(path.join(temporary, "tracked.txt"), "dirty\n");
  const dirty = spawnSync("sh", ["-c", wrangler.build.command], {
    cwd: temporary,
    encoding: "utf8",
  });
  assert.notEqual(dirty.status, 0);
});
