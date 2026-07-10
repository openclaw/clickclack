---
read_when:
  - deploying the isolated FakeCo test chat
  - testing ClickClack through OpenClaw and ClawRouter
  - tearing down FakeCo chat state
---

# FakeCo staging

FakeCo uses ClickClack's existing single-container runtime on one small AWS VM.
It does not introduce an AWS-native ClickClack platform. Production remains the
separate Cloudflare Worker and Cloudflare Container deployment in
`wrangler.jsonc`; this runbook never targets that Worker, its domains, its
Postgres database, or its R2 bucket.

The supported test topology is:

```text
browser / canary CLI ---> ClickClack VM <--- polls --- OpenClaw gateway VM
                              |                           |
                         SQLite volume                   +---> isolated ClawRouter
```

ClickClack is passive in this relationship. OpenClaw's ClickClack extension
polls chat events with a scoped bot token, dispatches the human message through
its configured ClawRouter provider, and posts the answer back as a quoted bot
message. ClickClack never needs the Gateway or ClawRouter credential.

## Files

- `deploy/fakeco/compose.yaml` — isolated Docker Compose project, volume,
  readiness check, resource bounds, and opt-in metadata metrics.
- `deploy/fakeco/.env.example` — non-secret instance values.
- `deploy/fakeco/openclaw.config.jsonc` — OpenClaw channel, gateway auth, and
  ClawRouter target contract. Every credential is an env-backed SecretRef.
- `deploy/fakeco/aws/` — locked single-VM CloudFormation owner, deterministic
  SSM bootstrap, backup/restore contract, and local tests.
- `.github/workflows/fakeco-aws.yml` — manual protected-Environment operations:
  `plan`, `apply`, `verify`, `teardown-plan`, and `teardown`.

## VM shape and network

The guarded AWS owner locks one `t4g.small` ARM64 VM (2 vCPU, 2 GiB RAM) and an
encrypted 16 GiB `gp3` root volume in `us-west-2`. It requires IMDSv2, assigns
no public IP or SSH key, and uses SSM for administration. A persistent 2 GiB
swap file keeps the initial Node + Go image build inside this small shape; CPU
credits use cost-bounded standard mode.

The owner reuses an explicitly supplied VPC, private subnet, and existing NAT
or transit gateway. Prefer the current `crabhelm-fakeco` stack's `VpcId`,
`ApplicationSubnetA`, and `NatGateway` resources. It never creates another NAT.
TCP `8080` is allowed only from the supplied OpenClaw gateway security group
and an optional metrics security group. See the complete [AWS owner
runbook](../deploy/fakeco/aws/README.md).

SQLite is the supported small-instance store. Run one application replica;
do not place multiple containers over the same SQLite volume. Move to the
existing Postgres adapter only if the test outgrows one VM.

## Prepare, seed, and start

These are deployment instructions only; repository validation does not run
them against a cloud account.

The AWS `apply` operation performs these same steps through bounded SSM and
stores seed-equality, endpoint, metrics, backup, and integrity evidence. The
commands below remain useful for local or inside-VM diagnosis.

```sh
cd deploy/fakeco
cp .env.example .env
# Set CLICKCLACK_PUBLIC_URL and, if needed, the private bind address in .env.

docker compose build
docker compose --profile tools run --rm seed > seed-manifest.json
docker compose up -d app
docker compose ps
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

`admin fakeco seed` creates three synthetic humans, the `fakeco` workspace,
four channels (`general`, `engineering`, `incidents`, `e2e-canary`), and three
small seeded threads. Fixed identity subjects and message nonces make reruns
idempotent. The JSON manifest contains only synthetic metadata and generated
resource IDs; it contains no token.

The app has `dev-bootstrap=false`. Create a service bot after startup with the
workspace ID and the first user ID from the manifest. The first user is the
workspace owner created by the deterministic seed:

```sh
docker compose exec app clickclack admin bot create \
  --workspace wsp_replace_from_manifest \
  --created-by usr_first_user_from_manifest \
  --name "FakeCo OpenClaw" \
  --handle fakeco-openclaw \
  --scopes bot:write \
  --token-name fakeco-gateway \
  --plain
```

The command reveals the token once. Send it directly to the approved secret
store as `CLICKCLACK_FAKECO_BOT_TOKEN`; never put it in `.env`, the seed
manifest, shell history, logs, tickets, or chat. OpenClaw resolves the bot user
ID through `/api/me`, so no second identifier needs to be configured.

## OpenClaw and ClawRouter contract

Render `openclaw.config.jsonc` on the OpenClaw VM with these non-secret values:

- `CLICKCLACK_FAKECO_BASE_URL` — private ClickClack origin.
- `CLAWROUTER_BASE_URL` — isolated staging ClawRouter origin.
- `CLAWROUTER_MODEL_ID` — one credential-granted catalog ID such as
  `openai/gpt-5.5`, without the leading `clawrouter/`. The template expands it
  to the canonical `clawrouter/<catalog-provider>/<catalog-model>` form.

Inject these credentials from the OpenClaw process's approved secret provider:

- `CLICKCLACK_FAKECO_BOT_TOKEN`
- `CLAWROUTER_API_KEY`
- `OPENCLAW_GATEWAY_TOKEN`

The committed template contains SecretRef objects, never values. In particular,
`models.providers.clawrouter.apiKey` resolves the env SecretRef
`CLAWROUTER_API_KEY`. ClawRouter owns upstream provider credentials; none
belong on the ClickClack or OpenClaw VMs.

Merge the template into the isolated OpenClaw config without replacing an
existing `plugins.allow`. When that allowlist exists, preserve its entries and
include both `clawrouter` and `clickclack`; keep
`plugins.entries.clawrouter.enabled=true`.

After OpenClaw starts, require all of these operator checks before the chat
canary:

```sh
curl -fsS "${CLAWROUTER_BASE_URL%/}/v1/health"
curl -fsS http://127.0.0.1:18789/healthz
curl -fsS http://127.0.0.1:18789/readyz
openclaw models status --probe --probe-provider clawrouter \
  --probe-max-tokens 8 --json
openclaw agent --agent main --model clawrouter/openai/gpt-5.5 \
  --message "Reply exactly: CLAWROUTER_CANARY_OK" --json
openclaw channels status --probe --channel clickclack
```

ClawRouter's `/v1/health` proves router liveness. Gateway `/healthz` is shallow
liveness; `/readyz` proves startup readiness. The model probe verifies the
configured provider credential and catalog path, while the direct agent canary
proves inference through the canonical model syntax before ClickClack adds the
channel round trip.

## End-to-end canary

The canary must authenticate as a synthetic human because OpenClaw correctly
ignores bot-authored events. Mint and consume a magic link for one seeded human,
then store the resulting session token in the approved secret store used by the
canary job. Inject it only as `CLICKCLACK_TOKEN`.

```sh
CLICKCLACK_SERVER=https://chat.fakeco.example \
CLICKCLACK_WORKSPACE=fakeco \
CLICKCLACK_CHANNEL=e2e-canary \
OPENCLAW_GATEWAY_HEALTH_URL=http://openclaw.fakeco.internal:18789/healthz \
clickclack canary --run-id fakeco-smoke-20260709 --json
```

The command first checks the configured Gateway health URL, then posts a unique
human message and polls for an ordinary bot reply that quotes that exact
message and equals `fakeco-canary-ok <correlation-id>`. It exits non-zero on
gateway failure, wrong credentials, a missing workspace/channel, a bot caller,
or timeout. The correlation ID is also sent on every HTTP request as
`X-Correlation-ID`, returned by ClickClack in the response header, and retained
as optional metadata on the durable creation event. `--run-id` adds an external
run identifier to JSON evidence only; `case_id` always equals the canary request
message ID. Neither value adds a new outbound gateway call.

## Health, logs, and telemetry

- `/healthz` is process liveness.
- `/readyz` verifies database connectivity and returns `503` without exposing
  the underlying error.
- `/metrics` exists only with `CLICKCLACK_METRICS_ENABLED=true`. Keep it on the
  private network. It exposes build/environment labels, readiness, request
  counts, status classes, normalized route patterns, and aggregate durations.
- Request logs contain method, host, normalized route pattern, protocol, remote
  address, correlation ID, status, bytes, and elapsed time.

Metrics and request logs never include authorization headers, cookies, query
values, request/response bodies, user/channel/message IDs, prompts, completions,
or tool output. The synthetic canary prompt and reply remain normal ClickClack
database rows, not telemetry payloads.

## Backup and teardown

For a retained test, run an online SQLite backup before stopping the service.
`docker compose down` removes containers and the network but preserves the
named `clickclack-fakeco-data` volume.

For AWS, run `teardown-plan` first. Guarded `teardown` then requires the exact
confirmation string and protected-Environment approval, creates a fresh hot
backup with `PRAGMA integrity_check`, waits for an encrypted EBS snapshot,
writes a versioned retained-resource manifest, and only then performs standard
stack deletion. The root EBS mapping has `DeleteOnTermination=false`; the
workflow proves that the volume remains `available` and the snapshot remains
`completed`. It never deletes a volume, snapshot, backup, manifest, or Docker
volume.

Before teardown, disable the OpenClaw ClickClack account and revoke or rotate
its ClickClack, ClawRouter, Gateway, and canary-human credentials in their
external secret stores. Final disposal of retained AWS data is a separate
account-owner action. Verify the production Cloudflare Worker, domains,
Postgres, and R2 never appear in the plan or teardown set.
