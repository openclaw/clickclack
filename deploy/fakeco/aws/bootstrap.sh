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
requested_source_commit="$CLICKCLACK_SOURCE_COMMIT"
runtime_source_commit="$CLICKCLACK_SOURCE_COMMIT"
runtime_source_sha256="$CLICKCLACK_SOURCE_SHA256"
release="$release_root/$runtime_source_commit"
image_name="clickclack:fakeco-$runtime_source_commit"
image_state="$state_root/image-$runtime_source_commit.id"
compose_override="$runtime_override"
backup_runtime_override="$state_root/compose.backup.yaml"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-${CLICKCLACK_SOURCE_COMMIT:0:12}"
log_file="$log_root/$run_id.log"
stage=initialize
pre_update_backup=false
captured_backup_path=
captured_backup_sha=
captured_backup_size=
captured_backup_key=
captured_backup_head=
readonly aws_cli_version=2.35.20
readonly aws_cli_archive_sha256=58799ce9276d4e8815fd19e4dc35649626c6b4fbd4d0e3df7433af9cfde41882
readonly max_single_put_bytes=5000000000

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
    -f "$compose_override" \
    "$@"
}

set_runtime_paths() {
  release="$release_root/$runtime_source_commit"
  image_name="clickclack:fakeco-$runtime_source_commit"
  image_state="$state_root/image-$runtime_source_commit.id"
}

resolve_backup_runtime() {
  stage=resolve-backup-runtime
  local container_id configured_image running_image_id recorded_image_id
  container_id="$(docker ps \
    --filter 'label=com.docker.compose.project=clickclack-fakeco' \
    --filter 'label=com.docker.compose.service=app' \
    --format '{{.ID}}')" || return 1
  [[ -n "$container_id" && "$container_id" != *$'\n'* ]] || return 1
  configured_image="$(docker inspect --format '{{.Config.Image}}' "$container_id")" || return 1
  [[ "$configured_image" =~ ^clickclack:fakeco-([0-9a-f]{40})$ ]] || return 1
  runtime_source_commit="${BASH_REMATCH[1]}"
  set_runtime_paths
  [[ -d "$release" && -f "$release/.source.sha256" && -f "$image_state" ]] || return 1
  runtime_source_sha256="$(<"$release/.source.sha256")"
  [[ "$runtime_source_sha256" =~ ^[0-9a-f]{64}$ ]] || return 1
  recorded_image_id="$(<"$image_state")"
  [[ "$recorded_image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  running_image_id="$(docker inspect --format '{{.Image}}' "$container_id")" || return 1
  [[ "$running_image_id" == "$recorded_image_id" ]] || return 1
  [[ "$(docker image inspect --format '{{.Id}}' "$image_name")" == "$recorded_image_id" ]] || return 1
  [[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_name")" == "$runtime_source_commit" ]] || return 1
  {
    printf '%s\n' 'services:'
    printf '%s\n' '  app:'
    printf '    image: "%s"\n' "$image_name"
    printf '%s\n' '  seed:'
    printf '    image: "%s"\n' "$image_name"
  } >"$backup_runtime_override" || return 1
  chmod 0600 "$backup_runtime_override" || return 1
  compose_override="$backup_runtime_override"
}

verify_persistent_runtime_config() {
  local configured_images requested_images runtime_images
  [[ -f "$runtime_env" && -f "$runtime_override" ]] || return 1
  configured_images="$(grep -c '^    image:' "$runtime_override" || true)"
  [[ "$configured_images" == "2" ]] || return 1
  if [[ "$action" == "backup" || "$pre_update_backup" == "true" ]]; then
    grep -Eq "^CLICKCLACK_WEB_VERSION=($requested_source_commit|$runtime_source_commit)$" "$runtime_env" || return 1
    requested_images="$(grep -Fxc "    image: \"clickclack:fakeco-$requested_source_commit\"" "$runtime_override" || true)"
    if [[ "$requested_source_commit" == "$runtime_source_commit" ]]; then
      [[ "$requested_images" == "2" ]] || return 1
    else
      runtime_images="$(grep -Fxc "    image: \"clickclack:fakeco-$runtime_source_commit\"" "$runtime_override" || true)"
      [[ "$requested_images" == "2" || "$runtime_images" == "2" ]] || return 1
    fi
  else
    grep -Fx "CLICKCLACK_WEB_VERSION=$runtime_source_commit" "$runtime_env" >/dev/null || return 1
    [[ "$(grep -Fxc "    image: \"$image_name\"" "$runtime_override" || true)" == "2" ]] || return 1
  fi
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
    [[ "$(<"$release/.source.sha256")" == "$runtime_source_sha256" ]]
  else
    printf '%s\n' "$runtime_source_sha256" >"$candidate/.source.sha256"
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
    printf 'CLICKCLACK_WEB_VERSION=%s\n' "$runtime_source_commit"
  } >"$runtime_env"
  {
    printf '%s\n' 'services:'
    printf '%s\n' '  app:'
    printf '    image: "%s"\n' "$image_name"
    printf '%s\n' '    build:'
    printf '%s\n' '      labels:'
    printf '        org.opencontainers.image.revision: "%s"\n' "$runtime_source_commit"
    printf '%s\n' '  seed:'
    printf '    image: "%s"\n' "$image_name"
    printf '%s\n' '    build:'
    printf '%s\n' '      labels:'
    printf '        org.opencontainers.image.revision: "%s"\n' "$runtime_source_commit"
  } >"$runtime_override"
  chmod 0640 "$runtime_env"
  chmod 0640 "$runtime_override"
}

build_and_start() {
  stage=build
  compose build --pull app
  image_id="$(docker image inspect --format '{{.Id}}' "$image_name")"
  [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]
  [[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_name")" == "$runtime_source_commit" ]]
  printf '%s\n' "$image_id" >"$image_state"
  stage=start
  compose up -d app
  verify_running_image
}

verify_running_image() {
  local container_id
  stage=runtime-identity
  [[ -f "$release/.source.sha256" ]] || return 1
  [[ "$(<"$release/.source.sha256")" == "$runtime_source_sha256" ]] || return 1
  [[ -f "$image_state" ]] || return 1
  verify_persistent_runtime_config
  image_id="$(<"$image_state")"
  [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  [[ "$(docker image inspect --format '{{.Id}}' "$image_name")" == "$image_id" ]] || return 1
  [[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_name")" == "$runtime_source_commit" ]] || return 1
  container_id="$(compose ps -q app)"
  [[ -n "$container_id" && "$container_id" != *$'\n'* ]] || return 1
  [[ "$(docker inspect --format '{{.State.Running}}' "$container_id")" == "true" ]] || return 1
  [[ "$(docker inspect --format '{{.Image}}' "$container_id")" == "$image_id" ]] || return 1
  [[ "$(docker inspect --format '{{.Config.Image}}' "$container_id")" == "$image_name" ]] || return 1
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

cleanup_success() {
  local backup_path="$1"
  local candidate name stale_image_list
  stage=cleanup-success
  if [[ "$action" != "backup" ]]; then
    docker container prune --force \
      --filter 'label=com.docker.compose.project=clickclack-fakeco'
    stale_image_list="$(docker image ls \
      --filter 'reference=clickclack:fakeco-*' \
      --format '{{.Repository}}:{{.Tag}}')"
    if [[ -n "$stale_image_list" ]]; then
      while IFS= read -r candidate; do
        [[ "$candidate" =~ ^clickclack:fakeco-[0-9a-f]{40}$ ]]
        if [[ "$candidate" != "$image_name" ]]; then
          docker image rm "$candidate"
        fi
      done <<<"$stale_image_list"
    fi
    docker image prune --force
    docker builder prune --force

    for candidate in "$release_root"/*; do
      [[ -d "$candidate" ]] || continue
      name="${candidate##*/}"
      [[ "$name" =~ ^[0-9a-f]{40}$ ]] || continue
      if [[ "$candidate" != "$release" ]]; then
        rm -rf -- "$candidate"
      fi
    done
    for candidate in "$state_root"/image-*.id; do
      [[ -f "$candidate" ]] || continue
      name="${candidate##*/}"
      [[ "$name" =~ ^image-[0-9a-f]{40}\.id$ ]] || continue
      if [[ "$candidate" != "$image_state" ]]; then
        rm -f -- "$candidate"
      fi
    done
  fi
  rm -f -- "$backup_path"
}

cleanup_run_files() {
  stage=cleanup-run-files
  rm -f -- \
    "$state_root/seed-$run_id-first.json" \
    "$state_root/seed-$run_id-second.json" \
    "$state_root/seed-$run_id-first.json.sorted" \
    "$state_root/seed-$run_id-second.json.sorted" \
    "$state_root/health-$run_id.headers" \
    "$state_root/health-$run_id.json" \
    "$state_root/ready-$run_id.headers" \
    "$state_root/ready-$run_id.json" \
    "$state_root/metrics-$run_id.txt" \
    "$state_root/backup-head-$run_id.json" \
    "$state_root/app-$run_id.log" \
    "$log_file"
  if [[ "$action" == "backup" ]]; then
    rm -f -- "$backup_runtime_override"
  fi
}

capture_backup() {
  local backup_id="$1"
  local stage_prefix="$2"
  local container_path mount_path integrity
  [[ "$backup_id" =~ ^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$ ]]
  [[ -z "$stage_prefix" || "$stage_prefix" == "pre-update-" ]]
  stage="${stage_prefix}sqlite-backup"
  container_path="/app/data/backups/clickclack-$backup_id.db"
  compose exec -T app sh -c 'mkdir -p /app/data/backups'
  compose exec -T app clickclack backup --data /app/data --out "$container_path"
  mount_path="$(docker volume inspect clickclack-fakeco-data --format '{{.Mountpoint}}')"
  captured_backup_path="$mount_path/backups/clickclack-$backup_id.db"
  [[ -f "$captured_backup_path" ]]
  integrity="$(sqlite3 "$captured_backup_path" 'PRAGMA integrity_check;')"
  [[ "$integrity" == "ok" ]]
  captured_backup_sha="$(sha256sum "$captured_backup_path" | cut -d' ' -f1)"
  captured_backup_size="$(stat -c '%s' "$captured_backup_path")"
  [[ "$captured_backup_size" =~ ^[0-9]+$ ]]
  if ((captured_backup_size > max_single_put_bytes)); then
    printf 'backup size exceeds the FakeCo single-object limit (%s bytes)\n' \
      "$max_single_put_bytes" >&2
    return 1
  fi
  captured_backup_key="$CLICKCLACK_BACKUP_PREFIX/sqlite/$runtime_source_commit/clickclack-$backup_id.db"
  captured_backup_head="$state_root/backup-head-$backup_id.json"

  stage="${stage_prefix}upload-backup"
  aws s3api put-object \
    --bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --key "$captured_backup_key" \
    --body "$captured_backup_path" \
    --content-type application/vnd.sqlite3 \
    --metadata "sha256=$captured_backup_sha,source-commit=$runtime_source_commit" \
    --server-side-encryption aws:kms \
    --ssekms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN" \
    --if-none-match '*' \
    --output json >/dev/null
  aws s3api head-object \
    --bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --key "$captured_backup_key" \
    --output json >"$captured_backup_head"
  jq -e \
    --arg sha256 "$captured_backup_sha" \
    --arg source_commit "$runtime_source_commit" \
    --arg kms_key "$CLICKCLACK_DATA_KMS_KEY_ARN" \
    --argjson size "$captured_backup_size" \
    '.Metadata.sha256 == $sha256 and
     .Metadata["source-commit"] == $source_commit and
     .ServerSideEncryption == "aws:kms" and
     .SSEKMSKeyId == $kms_key and
     .ContentLength == $size' "$captured_backup_head" >/dev/null
}

create_pre_update_backup() {
  local backup_id backup_suffix manifest_key evidence_file evidence_sha evidence_size evidence_head
  backup_suffix="$(printf 'pre-update:%s:%s:%s' \
    "$run_id" "$runtime_source_commit" "$CLICKCLACK_OWNER_COMMIT" | sha256sum | cut -c1-12)"
  backup_id="${run_id%-*}-$backup_suffix"
  [[ "$backup_id" != "$run_id" ]]
  capture_backup "$backup_id" pre-update-
  manifest_key="$CLICKCLACK_BACKUP_PREFIX/manifests/$backup_id.json"
  evidence_file="$state_root/pre-update-evidence-$run_id.json"
  evidence_head="$state_root/pre-update-evidence-head-$run_id.json"
  jq -n \
    --arg action pre-update \
    --arg run_id "$backup_id" \
    --arg source_commit "$runtime_source_commit" \
    --arg requested_source_commit "$requested_source_commit" \
    --arg owner_commit "$CLICKCLACK_OWNER_COMMIT" \
    --arg image_id "$image_id" \
    --arg backup_bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --arg backup_key "$captured_backup_key" \
    --arg backup_sha256 "$captured_backup_sha" \
    --argjson backup_size "$captured_backup_size" \
    --arg manifest_bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --arg manifest_key "$manifest_key" \
    '{
      schema_version: 1,
      status: "passed",
      action: $action,
      run_id: $run_id,
      source_commit: $source_commit,
      requested_source_commit: $requested_source_commit,
      owner_commit: $owner_commit,
      runtime_commit_verified: true,
      image_id: $image_id,
      health: true,
      readiness: true,
      metrics_metadata_only: true,
      integrity_check: "ok",
      backup: {
        bucket: $backup_bucket,
        key: $backup_key,
        sha256: $backup_sha256,
        size_bytes: $backup_size
      },
      manifest: {bucket: $manifest_bucket, key: $manifest_key}
    }' >"$evidence_file"
  evidence_sha="$(sha256sum "$evidence_file" | cut -d' ' -f1)"
  evidence_size="$(stat -c '%s' "$evidence_file")"

  stage=pre-update-upload-evidence
  aws s3api put-object \
    --bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --key "$manifest_key" \
    --body "$evidence_file" \
    --content-type application/json \
    --metadata "sha256=$evidence_sha,source-commit=$runtime_source_commit,requested-source-commit=$requested_source_commit" \
    --server-side-encryption aws:kms \
    --ssekms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN" \
    --if-none-match '*' \
    --output json >/dev/null
  aws s3api head-object \
    --bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --key "$manifest_key" \
    --output json >"$evidence_head"
  jq -e \
    --arg sha256 "$evidence_sha" \
    --arg source_commit "$runtime_source_commit" \
    --arg requested_source_commit "$requested_source_commit" \
    --arg kms_key "$CLICKCLACK_DATA_KMS_KEY_ARN" \
    --argjson size "$evidence_size" \
    '.Metadata.sha256 == $sha256 and
     .Metadata["source-commit"] == $source_commit and
     .Metadata["requested-source-commit"] == $requested_source_commit and
     .ServerSideEncryption == "aws:kms" and
     .SSEKMSKeyId == $kms_key and
     .ContentLength == $size' "$evidence_head" >/dev/null
  rm -f -- "$captured_backup_path" "$captured_backup_head" "$evidence_file" "$evidence_head"
}

create_backup() {
  local manifest_key log_key
  capture_backup "$run_id" ""
  manifest_key="$CLICKCLACK_BACKUP_PREFIX/manifests/$run_id.json"
  log_key="$CLICKCLACK_LOG_PREFIX/runs/$run_id/owner.log"
  compose logs --no-color --tail 500 app >"$state_root/app-$run_id.log"

  evidence_file="$state_root/evidence-$run_id.json"
  jq -n \
    --arg action "$action" \
    --arg run_id "$run_id" \
    --arg source_commit "$runtime_source_commit" \
    --arg stack_source_commit "$requested_source_commit" \
    --arg owner_commit "$CLICKCLACK_OWNER_COMMIT" \
    --arg image_id "$image_id" \
    --arg seed_sha256 "$seed_sha256" \
    --arg backup_bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --arg backup_key "$captured_backup_key" \
    --arg backup_sha256 "$captured_backup_sha" \
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
      stack_source_commit: $stack_source_commit,
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
  aws s3api put-object \
    --bucket "$CLICKCLACK_BACKUP_BUCKET" \
    --key "$manifest_key" \
    --body "$evidence_file" \
    --content-type application/json \
    --server-side-encryption aws:kms \
    --ssekms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN" \
    --if-none-match '*' \
    --output json >/dev/null
  cleanup_success "$captured_backup_path"
  stage=upload-log
  aws s3 cp "$log_file" "s3://$CLICKCLACK_LOG_BUCKET/$log_key" \
    --only-show-errors \
    --content-type text/plain \
    --sse aws:kms \
    --sse-kms-key-id "$CLICKCLACK_DATA_KMS_KEY_ARN"
  cleanup_run_files
  cat "$evidence_file" >&3
}

runtime_state_exists() {
  local containers
  if [[ -e "$runtime_env" || -e "$runtime_override" || -e /var/lib/docker/volumes/clickclack-fakeco-data ]]; then
    return 0
  fi
  if command -v docker >/dev/null && systemctl is-active --quiet docker; then
    containers="$(docker ps -a \
      --filter 'label=com.docker.compose.project=clickclack-fakeco' \
      --filter 'label=com.docker.compose.service=app' \
      --format '{{.ID}}')"
    [[ -n "$containers" ]] && return 0
  fi
  return 1
}

prepare_pre_update_backup() {
  stage=pre-update-detection
  runtime_state_exists || return 0
  [[ -f "$runtime_env" && ! -L "$runtime_env" ]]
  [[ -f "$runtime_override" && ! -L "$runtime_override" ]]
  systemctl is-active --quiet docker
  pre_update_backup=true
  resolve_backup_runtime
  verify_running_image
  probe_service
  create_pre_update_backup
  rm -f -- "$backup_runtime_override"
  pre_update_backup=false
  runtime_source_commit="$requested_source_commit"
  runtime_source_sha256="$CLICKCLACK_SOURCE_SHA256"
  compose_override="$runtime_override"
  set_runtime_paths
  stage=pre-update-complete
}

if [[ "$action" == "bootstrap" ]]; then
  prepare_pre_update_backup
  install_runtime
  install_source
  write_runtime_config
  build_and_start
else
  stage=existing-runtime
  systemctl is-active --quiet docker
  if [[ "$action" == "backup" ]]; then
    resolve_backup_runtime
  else
    [[ -d "$release" ]]
  fi
  verify_running_image
fi

prove_seed_equality
probe_service
create_backup
