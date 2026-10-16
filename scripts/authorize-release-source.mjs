import { execFileSync } from "node:child_process";
import { appendFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const allowedSigners = fileURLToPath(
  new URL("../.github/release-allowed-signers", import.meta.url),
);

export function authorizeReleaseSource(repository, tag, workflowCommit, signers = allowedSigners) {
  if (
    !/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$/.test(
      tag,
    )
  ) {
    throw new Error("Release source must be a semantic version tag beginning with v.");
  }
  if (!/^[a-f0-9]{40}$/.test(workflowCommit)) {
    throw new Error("Trusted workflow source must be a full commit SHA.");
  }
  const git = (...args) =>
    execFileSync("git", ["-C", repository, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  const ref = `refs/tags/${tag}`;
  if (git("cat-file", "-t", ref) !== "tag") {
    throw new Error("Release source must be an annotated, signed tag.");
  }
  const tagObject = git("rev-parse", ref);
  git(
    "-c",
    "gpg.format=ssh",
    "-c",
    `gpg.ssh.allowedSignersFile=${signers}`,
    "verify-tag",
    tagObject,
  );
  const header = git("cat-file", "tag", tagObject).split("\n\n", 1)[0];
  if (!header.split("\n").includes(`tag ${tag}`) || !header.split("\n").includes("type commit")) {
    throw new Error("Signed tag must name the requested release and point directly to a commit.");
  }
  const commit = git("rev-parse", "--verify", `${tagObject}^{commit}`);
  // The workflow comes from protected main; only its reviewed history may be signed.
  git("merge-base", "--is-ancestor", commit, workflowCommit);
  return { tag, version: tag.slice(1), commit, tag_object: tagObject };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const result = authorizeReleaseSource(
      process.cwd(),
      process.env.RELEASE_TAG,
      process.env.WORKFLOW_SHA,
    );
    for (const [name, value] of Object.entries(result)) {
      appendFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}\n`);
    }
    console.log(
      `Authorized ${result.tag} at ${result.commit} using the trusted release-signing policy.`,
    );
  } catch {
    console.error(
      "Release source rejected: require a fleet-signed annotated tag in the trusted workflow's main history.",
    );
    process.exitCode = 1;
  }
}
