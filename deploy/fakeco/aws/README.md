---
read_when:
  - owning the isolated FakeCo ClickClack VM in AWS
  - reviewing FakeCo AWS permissions, backups, restore, or teardown
---

# FakeCo AWS owner

This directory owns one non-production ClickClack VM in the dedicated FakeCo
AWS member account. It does not create an AWS-native ClickClack platform and it
does not touch the production Cloudflare Worker, Container, domains, Postgres,
or R2 configuration.

The locked shape is:

- Region `us-west-2`, stack `clickclack-fakeco`, protected GitHub Environment
  `fakeco`.
- One private Ubuntu 24.04 ARM64 `t4g.small` with standard CPU credits, a 2 GiB
  build swap file, and one encrypted 16 GiB `gp3` root volume.
- IMDSv2 required, no public IP, no key pair, no port 22 rule, and SSM-only
  administration.
- TCP `8080` from one explicitly supplied OpenClaw gateway security group and,
  when configured, one metrics security group. There is no CIDR ingress.
- An existing VPC, private subnet, and NAT gateway or transit gateway. The
  stack never creates a VPC, subnet, Internet gateway, route table, NAT
  gateway, load balancer, DNS record, or another egress charge.
- A reduced inline SSM agent core role with Parameter Store reads removed, plus
  prefix-conditioned `ListBucket` for the remote-script subprefix,
  `GetObject` on one artifact prefix, `PutObject` on one log prefix,
  `GetObject`/`PutObject` on one backup prefix, and use of one KMS key. It has
  no application-secret access.

`profile.json` locks the contract, `template.json` owns the VM resources,
`owner.mjs` renders and verifies exact observations, and `bootstrap.sh` runs
through SSM. `.github/workflows/fakeco-aws.yml` is the only cloud entrypoint.

## Existing account foundation

Create these account resources outside this repository before enabling the
workflow. The workflow only verifies and reuses them:

1. The dedicated FakeCo member account and GitHub OIDC provider.
2. A protected `fakeco` GitHub Environment with required reviewers and
   deployment-branch policy restricted to protected `main`.
3. OIDC role
   `openclaw/fakeco/github/clickclack-owner` and CloudFormation service role
   `openclaw/fakeco/cloudformation/clickclack-service` in that account.
4. Permissions boundary
   `openclaw/fakeco/clickclack-workload-boundary` that permits no more than the
   instance-role actions declared in `template.json`.
5. One customer-managed symmetric KMS key in `us-west-2` for EBS and the three
   S3 destinations, with the account-delegation and role grants below.
6. Existing encrypted artifact, log, and backup buckets in `us-west-2`. One
   bucket may serve all three roles if its prefixes and policies remain
   pairwise disjoint: no prefix may equal, contain, or be contained by another.
   Backup-bucket versioning must be enabled.
7. An existing VPC, private subnet with `MapPublicIpOnLaunch=false`, and an
   explicit default route to an existing NAT or transit gateway. The subnet
   must offer `t4g.small`.
8. The OpenClaw gateway security group in the same VPC; optionally a private
   metrics collector security group.

The GitHub OIDC role needs read-only preflight calls for STS, IAM, EC2, S3,
KMS, SSM, and CloudFormation, including `ec2:DescribeNetworkInterfaces`,
`cloudformation:GetTemplate`, and `cloudformation:DescribeStackResources`;
`iam:SimulatePrincipalPolicy` on only the two owner roles; `iam:PassRole` only
for the exact CloudFormation service role; change-set and stack lifecycle access
only for `clickclack-fakeco`; SSM Run Command only for that stack's instance; artifact
`PutObject`/`GetObject`/`HeadObject`; backup/log `HeadObject` and list access;
EBS snapshot create/describe; and no secret-read actions. The CloudFormation
service role needs only the EC2 security group, instance, IAM role/profile,
and tag operations represented in `template.json`, constrained by the
workload permissions boundary.

The KMS key policy must delegate the required actions to target-account IAM
policies through an unconditional `arn:aws:iam::<account-id>:root` principal
statement on resource `*`. That statement may use `kms:*`, or the narrower
set `kms:CreateGrant`, `kms:Decrypt`, `kms:DescribeKey`, `kms:Encrypt`,
`kms:GenerateDataKey*`, `kms:GetKeyPolicy`, and `kms:ReEncrypt*`. The identity
policies remain the least-privilege control:

- On the exact key, the GitHub OIDC role needs `kms:GetKeyPolicy` plus
  `kms:Decrypt`, `kms:DescribeKey`, `kms:GenerateDataKey`,
  `kms:GenerateDataKeyWithoutPlaintext`, and `kms:ReEncrypt*`. `Decrypt` and
  `GenerateDataKey` support the workflow's SSE-KMS downloads/uploads;
  `GetKeyPolicy` and `DescribeKey` support preflight; the remaining actions
  support its retained EBS snapshot.
- On the exact key, the CloudFormation service role needs `kms:Decrypt`,
  `kms:DescribeKey`, `kms:GenerateDataKeyWithoutPlaintext`, and
  `kms:ReEncrypt*` for the encrypted root volume.
- Both roles need `kms:CreateGrant` on the exact key only when
  `kms:GrantIsForAWSResource` is `true`. Unconditioned `CreateGrant` must remain
  denied.

Preflight reads and validates the key policy, then uses IAM policy simulation
to require every role/action/resource decision above and to prove that
`CreateGrant` is allowed only with the AWS-resource condition. The simulation
does not execute a KMS operation. AWS documents
[account IAM-policy delegation](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-default.html),
[S3 SSE-KMS permissions](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html),
and [EBS encryption permissions](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-encryption-requirements.html).

The IAM read set must include `GetInstanceProfile`, `GetRole`,
`ListAttachedRolePolicies`, `ListRolePolicies`, and `GetRolePolicy`. Every
verify and teardown operation replays the live instance-profile association,
role trust policy, permissions boundary, tags, absence of managed policies,
and both exact inline policy documents before SSM runs.

## Protected Environment variables

All workflow inputs are non-secret IDs, ARNs, names, or prefixes. Define them
as Environment variables, not GitHub secrets:

| Variable | Contract |
|---|---|
| `FAKECO_AWS_ACCOUNT_ID` | Exact 12-digit member-account ID. |
| `FAKECO_AWS_REGION` | Exactly `us-west-2`. |
| `FAKECO_GITHUB_ROLE_ARN` | Exact OIDC role path above. |
| `FAKECO_CLOUDFORMATION_SERVICE_ROLE_ARN` | Exact service-role path above. |
| `FAKECO_WORKLOAD_PERMISSIONS_BOUNDARY_ARN` | Exact policy path above. |
| `FAKECO_NETWORK_STACK_NAME` | Prefer `crabhelm-fakeco`; leave empty for explicit network IDs. |
| `FAKECO_VPC_ID` | Required without the network stack; if also supplied with it, must match its `VpcId` output. |
| `FAKECO_PRIVATE_SUBNET_ID` | Required without the network stack; if also supplied with it, must match `ApplicationSubnetA`. |
| `FAKECO_EGRESS_RESOURCE_ID` | Existing `nat-...` or `tgw-...`; if also supplied with Crabhelm, must match `NatGateway`. |
| `FAKECO_OPENCLAW_GATEWAY_SECURITY_GROUP_ID` | Required same-VPC source for TCP `8080`. |
| `FAKECO_METRICS_SECURITY_GROUP_ID` | Optional same-VPC source for TCP `8080`; must differ from the gateway group. |
| `FAKECO_AMI_ID` | Exact Canonical Ubuntu 24.04 ARM64 EBS AMI owned by `099720109477`. |
| `FAKECO_ARTIFACT_BUCKET` / `FAKECO_ARTIFACT_PREFIX` | Existing bucket and normalized prefix such as `clickclack/fakeco/artifacts`. |
| `FAKECO_LOG_BUCKET` / `FAKECO_LOG_PREFIX` | Existing bucket and normalized prefix such as `clickclack/fakeco/logs`. |
| `FAKECO_BACKUP_BUCKET` / `FAKECO_BACKUP_PREFIX` | Versioned bucket and normalized prefix such as `clickclack/fakeco/backups`. |
| `FAKECO_DATA_KMS_KEY_ARN` | Exact enabled customer-managed key ARN in the account and Region. |

Resolve and review a current AMI before setting the variable:

```sh
aws ssm get-parameter \
  --region us-west-2 \
  --name /aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id \
  --query Parameter.Value \
  --output text
```

The workflow then verifies owner, name, architecture, root device, EBS, HVM,
ENA, and availability. It never accepts an x86_64 AMI.

### Crabhelm network reuse

When `FAKECO_NETWORK_STACK_NAME=crabhelm-fakeco`, the owner reads the current
Crabhelm `VpcId` output and resolves its `ApplicationSubnetA` and `NatGateway`
logical resources. Current Crabhelm does not expose the latter two as outputs,
so the resource lookup is deliberate and exact. The ClickClack owner still
requires an explicit OpenClaw gateway security group; inferring that trust
boundary from another stack is unsafe.

Without Crabhelm, supply all three network IDs. Preflight proves that the
subnet belongs to the VPC, does not map public IPs, has exactly one applicable
route table, and has an active `0.0.0.0/0` route to the supplied available NAT or transit
gateway.

## Local render and tests

No AWS credentials are needed for the local contract tests:

```sh
pnpm test:fakeco-aws
actionlint .github/workflows/fakeco-aws.yml
```

For a local render, export only the Environment variables above, then run:

```sh
node deploy/fakeco/aws/owner.mjs render \
  --phase plan \
  --commit 1ef89aafc874f267e2a432c633148b1c1b200d2a \
  --owner-commit "$(git rev-parse HEAD)" \
  --output /tmp/clickclack-fakeco-rendered.json

node deploy/fakeco/aws/owner.mjs parameters \
  --rendered /tmp/clickclack-fakeco-rendered.json \
  --output /tmp/clickclack-fakeco-parameters.json
```

The rendered file contains resource IDs and should remain an operator-local
artifact. It contains no credentials or secret values.

## Manual workflow

Run only from protected `main`, after the `fakeco` Environment and account
foundation are ready:

```sh
# Read-only preflight plus a CloudFormation change set; never executes it.
gh workflow run fakeco-aws.yml --ref main \
  -f operation=plan \
  -f source_commit=1ef89aafc874f267e2a432c633148b1c1b200d2a

# Same preflight and change-set inspection, then protected execution.
gh workflow run fakeco-aws.yml --ref main \
  -f operation=apply \
  -f source_commit=1ef89aafc874f267e2a432c633148b1c1b200d2a \
  -f confirm=clickclack-fakeco

# Exact resource replay plus SSM seed/probe/backup verification.
gh workflow run fakeco-aws.yml --ref main \
  -f operation=verify \
  -f source_commit=1ef89aafc874f267e2a432c633148b1c1b200d2a

# Read-only retained-resource inventory.
gh workflow run fakeco-aws.yml --ref main \
  -f operation=teardown-plan \
  -f source_commit=1ef89aafc874f267e2a432c633148b1c1b200d2a

# Fresh hot backup + integrity check + encrypted snapshot + versioned manifest,
# followed by STANDARD stack deletion. Data is retained.
gh workflow run fakeco-aws.yml --ref main \
  -f operation=teardown \
  -f source_commit=1ef89aafc874f267e2a432c633148b1c1b200d2a \
  -f confirm=destroy-clickclack-fakeco-retain-data
```

`plan` is the default and uploads nothing. `apply` creates and prints a
metadata-only change set before any execution, uploads a deterministic
`git archive | gzip --no-name` source artifact, and verifies its SHA-256 and
SSE-KMS metadata. The source commit must be a full commit reachable from the
workflow's protected `main`. Existing immutable artifact keys are reused only
when both their stored metadata and downloaded bytes match the locally built
SHA-256. `apply` rejects every resource removal and every actual or conditional
replacement. An update that needs instance replacement must use guarded
`teardown` to retain the live backup, root volume, snapshot, and manifest, then
run a fresh `plan` and `apply`.

After CloudFormation completes, the workflow enables termination protection,
requires the VPC, subnet, ingress-source groups, and egress gateway to belong to
the dedicated account, matches the live template and logical-resource inventory
exactly, replays the live ingress rules, and rejects every CIDR, prefix list,
unexpected port or protocol, source account, and unapproved source security
group; it also replays the live instance profile and role policy boundary,
waits at most five minutes for SSM, and caps both the remote SSM script and its
workflow poll at forty minutes. Teardown polls the retained snapshot for at
most thirty minutes, and the complete job is capped at ninety minutes. First
apply normally takes 15–30 minutes because the ARM VM builds the pinned
multi-stage Docker image. `verify` normally takes 3–10 minutes. Snapshot
duration makes teardown roughly 10–30 minutes; all bounds fail closed.

## Bootstrap and proof

SSM fetches the bootstrap script from the exact artifact prefix with
`AWS-RunRemoteScript`. Its command line checks the downloaded bytes against the
workflow's expected SHA-256 before invoking `bash`, so a swapped object never
executes. Its required bucket listing is limited by `s3:prefix` to
`<artifact-prefix>/owner/*`; it cannot list unrelated bucket keys. The instance
profile cannot fetch GitHub, Parameter Store, or secrets.

Before an update bootstrap installs source, rewrites runtime configuration, or
starts the requested image, it detects existing runtime state and verifies the
one running release and image. For every verified existing runtime, including a
same-commit retry, it probes the current service, takes an integrity-checked hot
SQLite backup, uploads the encrypted database and metadata-only pre-update
evidence under a distinct object ID, creates both objects only when their keys
do not already exist, and verifies both S3 objects. A pristine first apply skips
this step; collisions and partial or unverifiable existing state fail before new
code can start. This small-instance owner accepts backup objects up to
5,000,000,000 bytes; larger databases fail closed before upload or update and
need a separately designed multipart backup path.

Bootstrap:

1. Installs Docker, Compose, SQLite, and probe tools from Noble, then installs
   a pinned official AWS CLI v2 ARM64 archive after checking its locked
   SHA-256; confirms both the host package architecture and Docker report
   `arm64`.
2. Creates a persistent 2 GiB build-only swap file inside the encrypted root
   volume, then builds the repo's digest-pinned multi-architecture Dockerfile
   into a commit-specific image with a matching OCI revision label.
3. Runs `admin fakeco seed` twice, canonicalizes both manifests, requires byte
   equality, and verifies the expected three users, four channels, and seven
   seeded message IDs.
4. Starts one SQLite-backed application container and checks `/healthz`,
   `/readyz`, correlation echo, and opt-in `/metrics`.
5. Rejects metrics containing user/workspace/channel/message ID prefixes or
   known body terms.
6. Requires the release source hash, recorded image ID, OCI revision label,
   running container image ID, and configured image name to match the requested
   commit. A partial prior apply therefore fails closed instead of certifying a
   stale container.
7. Uses `clickclack backup`, runs `PRAGMA integrity_check`, hashes the file,
   and uploads the encrypted database, metadata-only evidence, and bounded safe
   logs to the exact prefixes.
8. Only after the backup and evidence are durable, removes the local backup.
   Apply and verify runs also remove stopped project containers, obsolete
   commit-scoped images/releases/image records, and unused image/build cache.
   The active commit remains intact; teardown preserves any failed update
   candidate until the retained-volume snapshot, and failures before durable
   retention preserve their local candidate and backup for diagnosis.

Evidence records commit IDs, verified image ID, run ID, seed-manifest hash,
boolean probe results, backup object key/hash, and log object key. It contains
no auth header, cookie, token, prompt, completion, message body, or secret
value.

## OpenClaw preflight and human canary

The ClickClack instance deliberately has no OpenClaw or ClawRouter credential.
Run the existing preflight and canary from the isolated OpenClaw host where its
approved secret provider already injects the human ClickClack session and
ClawRouter/Gateway credentials:

```sh
curl -fsS "${CLAWROUTER_BASE_URL%/}/v1/health"
curl -fsS http://127.0.0.1:18789/healthz
curl -fsS http://127.0.0.1:18789/readyz
openclaw models status --probe --probe-provider clawrouter \
  --probe-max-tokens 8 --json
openclaw agent --agent main --model clawrouter/openai/gpt-5.5 \
  --message "Reply exactly: CLAWROUTER_CANARY_OK" --json

CLICKCLACK_SERVER=http://CLICKCLACK_PRIVATE_IP:8080 \
CLICKCLACK_WORKSPACE=fakeco \
CLICKCLACK_CHANNEL=e2e-canary \
OPENCLAW_GATEWAY_HEALTH_URL=http://127.0.0.1:18789/healthz \
clickclack canary --run-id "fakeco-${RUN_ID}" --json
```

Inject `CLICKCLACK_TOKEN` only through the OpenClaw host's approved secret
provider; do not add it to GitHub, SSM command parameters, instance metadata,
the ClickClack role, or evidence. Successful JSON must have
`gateway_preflight=true`, one `correlation_id`, and
`case_id == request_message_id`. Store only that JSON evidence in the approved
metadata-proof destination.

## Backup and restore

Every apply/verify/teardown SSM run creates a new encrypted hot backup and
integrity manifest. Backups and manifests are immutable by key; the guarded
teardown manifest is written once before deletion and finalized as a new S3
object version after deletion.

For teardown, the owner derives the runtime commit from the one running Compose
app container, then verifies its commit-scoped image tag, image ID, image label,
state record, and release. This deliberately allows a retained backup after a
failed update advances the stack parameter while the prior verified release is
still serving. Evidence records both the stack commit and the observed runtime
commit, and the backup uses the observed commit's immutable image through a
temporary Compose override.

Restore is intentionally manual and SSM-administered. Start from an applied VM
at the backup evidence's `source_commit` (the observed runtime commit), run
`verify` to retain a fresh pre-restore backup, then open an SSM session and
perform these steps as root with the selected backup key and recorded SHA-256:

```sh
(
set -euo pipefail
umask 077
evidence=/path/to/selected-backup-evidence.json
runtime_commit="$(jq -er '.source_commit | strings | select(test("^[0-9a-f]{40}$"))' "$evidence")"
backup_bucket="$(jq -er '.backup.bucket | strings' "$evidence")"
backup_key="$(jq -er '.backup.key | strings' "$evidence")"
backup_sha256="$(jq -er '.backup.sha256 | strings | select(test("^[0-9a-f]{64}$"))' "$evidence")"
[[ "$backup_bucket" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]
[[ "$backup_key" =~ ^clickclack/fakeco/[a-z0-9/_-]+/sqlite/$runtime_commit/clickclack-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}\.db$ ]]
[[ "$backup_key" != *//* ]]
release="/opt/clickclack/releases/$runtime_commit"
runtime_env=/etc/clickclack-fakeco/runtime.env
runtime_override=/etc/clickclack-fakeco/compose.owner.yaml
test -d "$release"
test "$(grep -Fxc "    image: \"clickclack:fakeco-$runtime_commit\"" "$runtime_override")" -eq 2
compose=(docker compose --project-directory "$release/deploy/fakeco" \
  --env-file "$runtime_env" -f "$release/deploy/fakeco/compose.yaml" \
  -f "$runtime_override")
mount_path="$(docker volume inspect clickclack-fakeco-data --format '{{.Mountpoint}}')"
restore_dir="/var/lib/clickclack-owner/restore-$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 0700 "$restore_dir"
aws s3 cp "s3://$backup_bucket/$backup_key" "$restore_dir/clickclack.db" --only-show-errors
printf '%s  %s\n' "$backup_sha256" "$restore_dir/clickclack.db" | sha256sum --check
test "$(sqlite3 "$restore_dir/clickclack.db" 'PRAGMA integrity_check;')" = ok
"${compose[@]}" stop app
owner_uid="$(stat -c %u "$mount_path/clickclack.db")"
owner_gid="$(stat -c %g "$mount_path/clickclack.db")"
install -d -m 0700 "$restore_dir/previous"
for file in clickclack.db clickclack.db-wal clickclack.db-shm; do
  test ! -e "$mount_path/$file" || cp -a -- "$mount_path/$file" "$restore_dir/previous/$file"
done
wait_ready() {
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null && \
      curl -fsS --max-time 2 http://127.0.0.1:8080/readyz >/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}
rollback_restore() {
  local exit_code=$?
  trap - ERR
  "${compose[@]}" stop app >/dev/null 2>&1 || true
  for file in clickclack.db clickclack.db-wal clickclack.db-shm; do
    rm -f -- "$mount_path/$file"
    test ! -e "$restore_dir/previous/$file" || \
      cp -a -- "$restore_dir/previous/$file" "$mount_path/$file"
  done
  if "${compose[@]}" up -d app >/dev/null 2>&1; then
    wait_ready || "${compose[@]}" stop app >/dev/null 2>&1 || true
  fi
  exit "$exit_code"
}
trap rollback_restore ERR
rm -f -- "$mount_path/clickclack.db" "$mount_path/clickclack.db-wal" "$mount_path/clickclack.db-shm"
install -o "$owner_uid" -g "$owner_gid" -m 0600 \
  "$restore_dir/clickclack.db" "$mount_path/clickclack.db"
"${compose[@]}" up -d app
wait_ready
trap - ERR
)
```

The previous database and WAL files remain in the restore directory. A failed
bounded readiness probe stops the restored app, reinstates those files, and
attempts to restart the prior database; failure to restart leaves the app
stopped. Run workflow `verify` again, then explicitly retain or remove those
files under the account's data-retention policy.

## Guarded teardown

`teardown-plan` is read-only. `teardown` requires the long confirmation string,
a protected-Environment approval, an exact resource replay, and a fresh SSM
backup. It then creates and waits for an encrypted EBS snapshot, writes and
verifies the retained-resource manifest, disables stack termination protection,
and uses only CloudFormation `STANDARD` deletion.

The root mapping has `DeleteOnTermination=false`; after stack deletion the
workflow waits until that encrypted volume is `available`, confirms the
snapshot remains `completed`, and finalizes the versioned manifest. There is
no force delete, `delete-volume`, `delete-snapshot`, S3 object deletion, or
`docker compose down --volumes` path. Final disposal of retained data is a
separate account-owner decision outside this workflow.

## Size, cost, and limits

`t4g.small` is the smallest locked shape for this owner: two ARM64 vCPUs and
2 GiB RAM. `t4g.micro` leaves too little memory for the Node + Go multi-stage
build even with swap. The Dockerfile's Node, Go, and Alpine images are pinned
multi-architecture manifests and the workflow verifies the live Docker daemon
is ARM64 before building.

At the public `us-west-2` Linux On-Demand rate of about `$0.0168/hour`, an
always-on VM is about `$12.26` per 730-hour month. A 16 GiB `gp3` volume at
`$0.08/GiB-month` is about `$1.28`, for roughly `$13.54/month` before S3,
snapshots, KMS requests, transfer, and tax. The template
uses standard rather than unlimited CPU credits, so excess CPU is throttled
instead of billed. The existing NAT gateway's fixed hourly cost is not added
by ClickClack; its incremental data processing and egress still apply. Recheck
the [official EC2 On-Demand pricing](https://aws.amazon.com/ec2/pricing/on-demand/)
and [EBS pricing](https://aws.amazon.com/ebs/pricing/) immediately before a
live run.
