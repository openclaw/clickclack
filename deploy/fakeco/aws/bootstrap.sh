#!/usr/bin/env bash
set -euo pipefail

umask 077

action="${1:-}"
case "$action" in
  bootstrap | verify | backup) ;;
  *)
    printf '%s\n' 'usage: bootstrap.sh <bootstrap|verify|backup>' >&2
    exit 64
    ;;
esac

required=(
  CLICKCLACK_SOURCE_COMMIT
  CLICKCLACK_OWNER_COMMIT
  CLICKCLACK_SOURCE_URI
  CLICKCLACK_SOURCE_SHA256
  CLICKCLACK_BACKUP_BUCKET
  CLICKCLACK_BACKUP_PREFIX
  CLICKCLACK_LOG_BUCKET
  CLICKCLACK_LOG_PREFIX
  CLICKCLACK_DATA_KMS_KEY_ARN
)
for name in "${required[@]}"; do
  [[ -n "${!name:-}" ]] || {
    printf 'missing required variable %s\n' "$name" >&2
    exit 64
  }
done

[[ "$CLICKCLACK_SOURCE_COMMIT" =~ ^[0-9a-f]{40}$ ]]
[[ "$CLICKCLACK_OWNER_COMMIT" =~ ^[0-9a-f]{40}$ ]]
[[ "$CLICKCLACK_SOURCE_SHA256" =~ ^[0-9a-f]{64}$ ]]
[[ "$CLICKCLACK_SOURCE_URI" =~ ^s3://[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]/clickclack/fakeco/[a-z0-9/_-]+\.tar\.gz$ ]]
[[ "$CLICKCLACK_BACKUP_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]
[[ "$CLICKCLACK_LOG_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]
[[ "$CLICKCLACK_BACKUP_PREFIX" =~ ^clickclack/fakeco/[a-z0-9/_-]+$ ]]
[[ "$CLICKCLACK_LOG_PREFIX" =~ ^clickclack/fakeco/[a-z0-9/_-]+$ ]]
[[ "$CLICKCLACK_DATA_KMS_KEY_ARN" =~ ^arn:aws:kms:us-west-2:[0-9]{12}:key/[0-9a-f-]{36}$ ]]

owner_root=/opt/clickclack-owner
release_root=/opt/clickclack/releases
state_root=/var/lib/clickclack-owner
log_root=/var/log/clickclack-fakeco
runtime_env=/etc/clickclack-fakeco/runtime.env
runtime_override=/etc/clickclack-fakeco/compose.owner.yaml
release="$release_root/$CLICKCLACK_SOURCE_COMMIT"
image_name="clickclack:fakeco-$CLICKCLACK_SOURCE_COMMIT"
image_state="$state_root/image-$CLICKCLACK_SOURCE_COMMIT.id"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-${CLICKCLACK_SOURCE_COMMIT:0:12}"
log_file="$log_root/$run_id.log"
stage=initialize
readonly aws_cli_version=2.35.20
readonly aws_cli_archive_sha256=58799ce9276d4e8815fd19e4dc35649626c6b4fbd4d0e3df7433af9cfde41882

install -d -m 0750 "$owner_root" "$release_root" "$state_root" "$log_root" "$(dirname "$runtime_env")"
touch "$log_file"
chmod 0600 "$log_file"
exec 3>&1
exec >>"$log_file" 2>&1

failure() {
  local code=$?
  jq -cn \
    --arg status failed \
    --arg action "$action" \
    --arg stage "$stage" \
    --arg run_id "$run_id" \
    --argjson exit_code "$code" \
    '{status:$status,action:$action,stage:$stage,run_id:$run_id,exit_code:$exit_code}' >&3 || true
  exit "$code"
}
trap failure ERR

compose() {
  docker compose \
    --project-directory "$release/deploy/fakeco" \
    --env-file "$runtime_env" \
    -f "$release/deploy/fakeco/compose.yaml" \
    -f "$runtime_override" \
    "$@"
}

install_aws_cli() {
  stage=install-aws-cli
  dpkg --print-architecture | grep -Fx arm64
  if /usr/local/bin/aws --version 2>&1 | grep -F "aws-cli/$aws_cli_version " >/dev/null; then
    return
  fi

  local work archive
  work="$(mktemp -d "$owner_root/aws-cli.XXXXXX")"
  archive="$work/awscliv2.zip"
  curl --proto '=https' --tlsv1.2 --fail --show-error --silent --location \
    --retry 3 --max-time 180 \
    "https://awscli.amazonaws.com/awscli-exe-linux-aarch64-$aws_cli_version.zip" \
    --output "$archive"
  printf '%s  %s\n' "$aws_cli_archive_sha256" "$archive" | sha256sum --check --status
  unzip -q "$archive" -d "$work"
  if [[ -x /usr/local/aws-cli/v2/current/bin/aws ]]; then
    "$work/aws/install" \
      --bin-dir /usr/local/bin \
      --install-dir /usr/local/aws-cli \
      --update
  else
    "$work/aws/install" \
      --bin-dir /usr/local/bin \
      --install-dir /usr/local/aws-cli
  fi
  rm -rf "$work"
  /usr/local/bin/aws --version 2>&1 | grep -F "aws-cli/$aws_cli_version "
}

install_runtime() {
  stage=install-runtime
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq \
    ca-certificates \
    curl \
    docker-compose-v2 \
    docker.io \
    gzip \
    jq \
    sqlite3 \
    tar \
    unzip
  install_aws_cli
  stage=install-runtime
  systemctl enable --now docker
  if ! swapon --show=NAME --noheadings | grep -Fx '/var/lib/clickclack-owner/build.swap' >/dev/null; then
    if [[ ! -f /var/lib/clickclack-owner/build.swap ]]; then
      fallocate -l 2G /var/lib/clickclack-owner/build.swap
      chmod 0600 /var/lib/clickclack-owner/build.swap
      mkswap /var/lib/clickclack-owner/build.swap >/dev/null
    fi
    if ! grep -Fx '/var/lib/clickclack-owner/build.swap none swap sw 0 0' /etc/fstab >/dev/null; then
      printf '%s\n' '/var/lib/clickclack-owner/build.swap none swap sw 0 0' >>/etc/fstab
    fi
    swapon /var/lib/clickclack-owner/build.swap
  fi
  docker version --format '{{.Server.Arch}}' | grep -Fx 'arm64'
  docker compose version
}

install_source() {
  stage=install-source
  local work archive candidate
  work="$(mktemp -d "$owner_root/source.XXXXXX")"
  archive="$work/source.tar.gz"
  candidate="$work/release"
  aws s3 cp "$CLICKCLACK_SOURCE_URI" "$archive" --only-show-errors
  printf '%s  %s\n' "$CLICKCLACK_SOURCE_SHA256" "$archive" | sha256sum --check --status
  mkdir "$candidate"
  tar -xzf "$archive" -C "$candidate"
  [[ -f "$candidate/Dockerfile" ]]
  [[ -f "$candidate/deploy/fakeco/compose.yaml" ]]
  if [[ -d "$release" ]]; then
    [[ "$(<"$release/.source.sha256")" == "$CLICKCLACK_SOURCE_SHA256" ]]
  else
    printf '%s\n' "$CLICKCLACK_SOURCE_SHA256" >"$candidate/.source.sha256"
    mv "$candidate" "$release"
  fi
  rm -rf "$work"
}

write_runtime_config() {
  stage=runtime-config
  local token private_ip
  token="$(curl -fsS --max-time 5 -X PUT \
    -H 'X-aws-ec2-metadata-token-ttl-seconds: 60' \
    http://169.254.169.254/latest/api/token)"
  private_ip="$(curl -fsS --max-time 5 \
    -H "X-aws-ec2-metadata-token: $token" \
    http://169.254.169.254/latest/meta-data/local-ipv4)"
  unset token
  [[ "$private_ip" =~ ^10\.|^172\.(1[6-9]|2[0-9]|3[01])\.|^192\.168\. ]]
  {
    printf 'CLICKCLACK_PUBLIC_URL=http://%s:8080\n' "$private_ip"
    printf 'CLICKCLACK_BIND_ADDR=0.0.0.0\n'
    printf 'CLICKCLACK_PORT=8080\n'
    printf 'CLICKCLACK_WEB_VERSION=%s\n' "$CLICKCLACK_SOURCE_COMMIT"
  } >"$runtime_env"
  {
    printf '%s\n' 'services:'
    printf '%s\n' '  app:'
    printf '    image: "%s"\n' "$image_name"
    printf '%s\n' '    build:'
    printf '%s\n' '      labels:'
    printf '        org.opencontainers.image.revision: "%s"\n' "$CLICKCLACK_SOURCE_COMMIT"
    printf '%s\n' '  seed:'
    printf '    image: "%s"\n' "$image_name"
    printf '%s\n' '    build:'
    printf '%s\n' '      labels:'
    printf '        org.opencontainers.image.revision: "%s"\n' "$CLICKCLACK_SOURCE_COMMIT"
  } >"$runtime_override"
  chmod 0640 "$runtime_env"
  chmod 0640 "$runtime_override"
}

build_and_start() {
  stage=build
  compose build --pull app
  image_id="$(docker image inspect --format '{{.Id}}' "$image_name")"
  [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]
  [[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_name")" == "$CLICKCLACK_SOURCE_COMMIT" ]]
  printf '%s\n' "$image_id" >"$image_state"
  stage=start
  compose up -d app
  verify_running_image
}

verify_running_image() {
  stage=runtime-identity
  [[ -f "$release/.source.sha256" ]]
  [[ "$(<"$release/.source.sha256")" == "$CLICKCLACK_SOURCE_SHA256" ]]
  [[ -f "$runtime_env" && -f "$runtime_override" && -f "$image_state" ]]
  grep -Fx "CLICKCLACK_WEB_VERSION=$CLICKCLACK_SOURCE_COMMIT" "$runtime_env" >/dev/null
  grep -Fx "    image: \"$image_name\"" "$runtime_override" >/dev/null
  image_id="$(<"$image_state")"
  [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]
  [[ "$(docker image inspect --format '{{.Id}}' "$image_name")" == "$image_id" ]]
  [[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_name")" == "$CLICKCLACK_SOURCE_COMMIT" ]]
  local container_id
  container_id="$(compose ps -q app)"
  [[ -n "$container_id" ]]
  [[ "$(docker inspect --format '{{.State.Running}}' "$container_id")" == "true" ]]
  [[ "$(docker inspect --format '{{.Image}}' "$container_id")" == "$image_id" ]]
  [[ "$(docker inspect --format '{{.Config.Image}}' "$container_id")" == "$image_name" ]]
}

prove_seed_equality() {
  stage=seed-rerun
  local first second first_sorted second_sorted
  first="$state_root/seed-$run_id-first.json"
  second="$state_root/seed-$run_id-second.json"
  first_sorted="$first.sorted"
  second_sorted="$second.sorted"
  compose --profile tools run --rm seed >"$first"
  compose --profile tools run --rm seed >"$second"
  jq -S . "$first" >"$first_sorted"
  jq -S . "$second" >"$second_sorted"
  cmp -s "$first_sorted" "$second_sorted"
  jq -e '
    .version == "fakeco.seed.v1" and
    .workspace.slug == "fakeco" and
    (.users | length) == 3 and
    (.channels | map(.name) | sort) == ["e2e-canary", "engineering", "general", "incidents"] and
    (.message_ids | length) == 7
  ' "$first_sorted" >/dev/null
  seed_sha256="$(sha256sum "$first_sorted" | cut -d' ' -f1)"
}

probe_service() {
  stage=service-probes
  local correlation health_headers health_body ready_headers ready_body metrics
  correlation="fakeco-owner-${run_id//:/-}"
  health_headers="$state_root/health-$run_id.headers"
  health_body="$state_root/health-$run_id.json"
  ready_headers="$state_root/ready-$run_id.headers"
  ready_body="$state_root/ready-$run_id.json"
  metrics="$state_root/metrics-$run_id.txt"
  for _ in $(seq 1 90); do
    if curl -fsS --max-time 3 -D "$health_headers" -o "$health_body" \
      -H "X-Correlation-ID: $correlation" http://127.0.0.1:8080/healthz &&
      curl -fsS --max-time 3 -D "$ready_headers" -o "$ready_body" \
        -H "X-Correlation-ID: $correlation" http://127.0.0.1:8080/readyz; then
      break
    fi
    sleep 2
  done
  jq -e '.status == "ok"' "$health_body" >/dev/null
  jq -e '.status == "ready"' "$ready_body" >/dev/null
  grep -qi "^X-Correlation-Id: $correlation" "$health_headers"
  grep -qi "^X-Correlation-Id: $correlation" "$ready_headers"
  curl -fsS --max-time 5 -o "$metrics" http://127.0.0.1:8080/metrics
  grep -F 'clickclack_ready 1' "$metrics"
  grep -F 'clickclack_build_info{environment="fakeco"' "$metrics"
  if grep -Eq 'wsp_|usr_|chn_|msg_|FakeCo canary|Welcome to FakeCo|prompt|completion' "$metrics"; then
    printf '%s\n' 'metrics contained forbidden high-cardinality or body content' >&2
    return 1
  fi
}

create_backup() {
  stage=sqlite-backup
  local container_path mount_path host_path integrity backup_sha backup_key manifest_key log_key
  container_path="/app/data/backups/clickclack-$run_id.db"
  compose exec -T app sh -c 'mkdir -p /app/data/backups'
  compose exec -T app clickclack backup --data /app/data --out "$container_path"
  mount_path="$(docker volume inspect clickclack-fakeco-data --format '{{.Mountpoint}}')"
  host_path="$mount_path/backups/clickclack-$run_id.db"
  [[ -f "$host_path" ]]
  integrity="$(sqlite3 "$host_path" 'PRAGMA integrity_check;')"
  [[ "$integrity" == "ok" ]]
  backup_sha="$(sha256sum "$host_path" | cut -d' ' -f1)"
  backup_key="$CLICKCLACK_BACKUP_PREFIX/sqlite/$CLICKCLACK_SOURCE_COMMIT/clickclack-$run_id.db"
  manifest_key="$CLICKCLACK_BACKUP_PREFIX/manifests/$run_id.json"
  log_key="$CLICKCLACK_LOG_PREFIX/runs/$run_id/owner.log"

  stage=upload-backup
  aws s3 cp "$host_path" "s3://$CLICKCLACK_BACKUP_BUCKET/$backup_key" \
    --only-show-errors \
    --sse aws:kms \
    --sse-kms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN"
  docker compose \
    --project-directory "$release/deploy/fakeco" \
    --env-file "$runtime_env" \
    -f "$release/deploy/fakeco/compose.yaml" \
    logs --no-color --tail 500 app >"$state_root/app-$run_id.log"

  evidence_file="$state_root/evidence-$run_id.json"
  jq -n \
    --arg action "$action" \
    --arg run_id "$run_id" \
    --arg source_commit "$CLICKCLACK_SOURCE_COMMIT" \
    --arg owner_commit "$CLICKCLACK_OWNER_COMMIT" \
    --arg image_id "$image_id" \
    --arg seed_sha256 "$seed_sha256" \
    --arg backup_bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --arg backup_key "$backup_key" \
    --arg backup_sha256 "$backup_sha" \
    --arg manifest_bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --arg manifest_key "$manifest_key" \
    --arg log_bucket "$CLICKCLACK_LOG_BUCKET" \
    --arg log_key "$log_key" \
    '{
      schema_version: 1,
      status: "passed",
      action: $action,
      run_id: $run_id,
      source_commit: $source_commit,
      owner_commit: $owner_commit,
      runtime_commit_verified: true,
      image_id: $image_id,
      seed_equal: true,
      seed_manifest_sha256: $seed_sha256,
      health: true,
      readiness: true,
      metrics_metadata_only: true,
      integrity_check: "ok",
      backup: {bucket: $backup_bucket, key: $backup_key, sha256: $backup_sha256},
      manifest: {bucket: $manifest_bucket, key: $manifest_key},
      safe_log: {bucket: $log_bucket, key: $log_key}
    }' >"$evidence_file"

  stage=upload-evidence
  aws s3 cp "$evidence_file" "s3://$CLICKCLACK_BACKUP_BUCKET/$manifest_key" \
    --only-show-errors \
    --content-type application/json \
    --sse aws:kms \
    --sse-kms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN"
  aws s3 cp "$log_file" "s3://$CLICKCLACK_LOG_BUCKET/$log_key" \
    --only-show-errors \
    --content-type text/plain \
    --sse aws:kms \
    --sse-kms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN"
  cat "$evidence_file" >&3
}

if [[ "$action" == "bootstrap" ]]; then
  install_runtime
  install_source
  write_runtime_config
  build_and_start
else
  stage=existing-runtime
  [[ -d "$release" ]]
  systemctl is-active --quiet docker
  verify_running_image
fi

prove_seed_equality
probe_service
create_backup
