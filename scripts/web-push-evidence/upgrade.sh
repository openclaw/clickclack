#!/usr/bin/env bash
# Upgrade evidence for the web push migrations on a populated database.
#
#   scripts/web-push-evidence/upgrade.sh \
#     --sqlite-snapshot PATH --postgres-admin-dsn DSN --out DIR \
#     [--base v0.5.1] [--previous 4fdae122]
#
# From the release: a copy of an existing SQLite database (the snapshot itself
# is only read), and a fresh PostgreSQL database migrated and seeded by the
# base release, are upgraded by head's `migrate`. For each, the run records
# the row count, a SHA-256 of the full contents, and the schema of
# user_notification_settings and channel_notification_settings before and
# after; checks that head applied every migration the database lacked and
# that user_push_subscriptions exists, is empty, and has its current columns;
# and replays Pushover recipient selection with the base release before the
# upgrade and with head after it. The two digests must match.
#
# From the previous head: a database the pull request's previous head
# migrated, holding devices of every kind registered through that head's
# store, is upgraded by head. Every device must keep its old columns byte for
# byte and read the defaults in the new ones. Head's server then boots on it
# with a VAPID key, and its start sweep must remove the devices whose session
# ended and keep the rest, and web push recipient selection must match what
# the previous head selected. Only counts, schema, migration names, device
# labels, and digests are written to DIR.
set -euo pipefail
export LC_ALL=C

base="v0.5.1"
previous="4fdae122"
snapshot=""
admin_dsn=""
out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --sqlite-snapshot) snapshot="$2"; shift 2 ;;
    --postgres-admin-dsn) admin_dsn="$2"; shift 2 ;;
    --out) out="$2"; shift 2 ;;
    --base) base="$2"; shift 2 ;;
    --previous) previous="$2"; shift 2 ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done
if [[ -z "$snapshot" || -z "$admin_dsn" || -z "$out" ]]; then
  echo "usage: $0 --sqlite-snapshot PATH --postgres-admin-dsn DSN --out DIR [--base TAG] [--previous REV]" >&2
  exit 2
fi
for tool in git go sqlite3 psql shasum; do
  command -v "$tool" >/dev/null || { echo "missing $tool" >&2; exit 2; }
done

repo="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/clickclack-upgrade-evidence.XXXXXX")"
pg_dbs=()
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; fi
  local db
  for db in ${pg_dbs[@]+"${pg_dbs[@]}"}; do
    psql "$admin_dsn" -qAtc "DROP DATABASE IF EXISTS $db" >/dev/null 2>&1 || true
  done
  rm -rf "$work"
}
trap cleanup EXIT
# The server's own settings must not leak into the runs below.
while IFS= read -r name; do unset "$name"; done < <(env | sed -n 's/^\(CLICKCLACK_[A-Z0-9_]*\)=.*/\1/p')

head_rev="$(git -C "$repo" rev-parse --short HEAD)"
base_rev="$(git -C "$repo" rev-parse --short "$base^{commit}")"
prev_rev="$(git -C "$repo" rev-parse --short "$previous^{commit}")"
summary="$out/summary.txt"
failures=0
note() { printf '%s\n' "$*" | tee -a "$summary"; }
check() {
  if [[ "$2" == "$3" ]]; then note "  ok    $1"; else note "  FAIL  $1: $2 then $3"; failures=$((failures + 1)); fi
}
: >"$summary"
note "Web push upgrade evidence"
note "head:     $(git -C "$repo" rev-parse HEAD)"
note "previous: $(git -C "$repo" rev-parse "$previous^{commit}") (the pull request's previous head)"
note "base:     $base ($(git -C "$repo" rev-parse "$base^{commit}"))"
if [[ -n "$(git -C "$repo" status --porcelain -- scripts/web-push-evidence)" ]]; then
  note "evidence scripts: modified in the work tree, not as committed at head"
else
  note "evidence scripts: as committed at head"
fi

for tree in base prev head; do
  rev="$base_rev"
  [[ "$tree" == prev ]] && rev="$prev_rev"
  [[ "$tree" == head ]] && rev="$head_rev"
  mkdir -p "$work/$tree"
  git -C "$repo" archive "$rev" | tar -x -C "$work/$tree"
  mkdir -p "$work/$tree/apps/api/internal/upgradeevidence"
  cp "$repo/scripts/web-push-evidence/upgradeevidence/evidence_test.go" "$work/$tree/apps/api/internal/upgradeevidence/"
  if [[ "$tree" != base ]]; then
    cp "$repo/scripts/web-push-evidence/upgradeevidence/push_test.go" "$work/$tree/apps/api/internal/upgradeevidence/"
  fi
  (cd "$work/$tree" && go build -o "$work/clickclack-$tree" ./apps/api/cmd/clickclack)
done

evidence() {
  local tree="$1" db="$2" mode="$3" result="${4:-}"
  (cd "$work/$tree" && CLICKCLACK_EVIDENCE_DB="$db" CLICKCLACK_EVIDENCE_MODE="$mode" CLICKCLACK_EVIDENCE_OUT="$result" \
    go test -tags clickclack_upgrade_evidence ./apps/api/internal/upgradeevidence -run TestUpgradeEvidence -count=1 >/dev/null)
}

digest_of() { sed -n 's/.*"digest": "\([0-9a-f]*\)".*/\1/p' "$1"; }
selected_of() { sed -n "s/.*\"$2\": \\([0-9]*\\).*/\\1/p" "$1"; }

sqlite_query() { sqlite3 "$1" "$2"; }
sqlite_table() { sqlite3 -separator '|' "$1" "SELECT * FROM $2 ORDER BY 1, 2" | shasum -a 256 | cut -c1-64; }
sqlite_count() { sqlite3 "$1" "SELECT COUNT(*) FROM $2"; }
sqlite_schema() { sqlite3 "$1" ".schema $2" | shasum -a 256 | cut -c1-16; }
sqlite_columns() { sqlite3 "$1" "SELECT name FROM pragma_table_info('$2') ORDER BY cid"; }
pg_query() { psql "$1" -qAtc "$2"; }
pg_table() { psql "$1" -qAtc "COPY (SELECT * FROM $2 ORDER BY 1, 2) TO STDOUT" | shasum -a 256 | cut -c1-64; }
pg_count() { psql "$1" -qAtc "SELECT COUNT(*) FROM $2"; }
pg_schema() {
  psql "$1" -qAtc "SELECT column_name || ' ' || data_type || ' ' || is_nullable || ' ' || coalesce(column_default, '') FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = '$2' ORDER BY ordinal_position" | shasum -a 256 | cut -c1-16
}
pg_columns() {
  psql "$1" -qAtc "SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = '$2' ORDER BY ordinal_position"
}

# The columns user_push_subscriptions had before head, and what head adds.
push_old_columns="id, user_id, endpoint, p256dh, auth, user_agent, session_token_hash, created_at, updated_at, last_success_at, next_attempt_at, failure_count"
push_new_columns="vapid_key_id failing_since last_failure_at"
push_kinds="healthy revoked expired session-deleted development failing-recent failing-old"
push_kept="development failing-old failing-recent healthy"
push_removed="expired revoked session-deleted"

# push_rows hashes the old columns of the devices whose label is one of the
# given kinds (every device with none), each value quoted with its type, so
# the hash moves if any byte of any old column does.
push_rows() {
  local engine="$1" db="$2" where="" kind
  shift 2
  if [[ $# -gt 0 ]]; then
    where="WHERE user_agent IN ("
    for kind in "$@"; do where+="'upgrade evidence $kind',"; done
    where="${where%,})"
  fi
  if [[ "$engine" == sqlite ]]; then
    local quoted
    quoted="$(printf '%s\n' "$push_old_columns" | tr ',' '\n' | sed 's/^ *\(.*\)$/quote(\1)/' | paste -sd, -)"
    sqlite3 -separator '|' "$db" "SELECT $quoted FROM user_push_subscriptions $where ORDER BY id"
  else
    psql "$db" -qAtc "COPY (SELECT $push_old_columns FROM user_push_subscriptions $where ORDER BY id) TO STDOUT"
  fi | shasum -a 256 | cut -c1-64
}
push_labels() {
  "${1}_query" "$2" "SELECT substr(user_agent, 18) FROM user_push_subscriptions WHERE user_agent LIKE 'upgrade evidence %' ORDER BY user_agent" | tr '\n' ' ' | sed 's/ $//'
}
push_defaults() {
  "${1}_query" "$2" "SELECT COUNT(*) FROM user_push_subscriptions WHERE vapid_key_id = '' AND failing_since IS NULL AND last_failure_at IS NULL"
}

migration_dir() { if [[ "$1" == sqlite ]]; then echo sqlite; else echo postgres; fi; }
recorded_migrations() { "${1}_query" "$2" "SELECT name FROM schema_migrations ORDER BY name"; }

# migrate_with runs a tree's migrate and notes what it applied. With check set,
# it checks that exactly the tree's migrations the database lacked were
# applied.
migrate_with() {
  local tree="$1" engine="$2" db="$3" url="$4" label="$5" checked="${6:-}"
  recorded_migrations "$engine" "$db" >"$work/migrations-before"
  "$work/clickclack-$tree" migrate --db "$url" >/dev/null
  recorded_migrations "$engine" "$db" >"$work/migrations-after"
  local applied expected
  applied="$(comm -13 "$work/migrations-before" "$work/migrations-after" | tr '\n' ' ' | sed 's/ $//')"
  note "  migrations applied by $label: $applied"
  if [[ -n "$checked" ]]; then
    expected="$(ls "$work/$tree/apps/api/internal/store/$(migration_dir "$engine")/migrations" | grep '\.sql$' | sort | comm -23 - "$work/migrations-before" | tr '\n' ' ' | sed 's/ $//')"
    check "$label applied every migration the database lacked, and nothing else" "$expected" "$applied"
  fi
}

settings_record() {
  local engine="$1" db="$2" file="$3" table
  : >"$file"
  for table in user_notification_settings channel_notification_settings; do
    printf '%s %s %s %s\n' "$table" "$("${engine}_count" "$db" "$table")" \
      "$("${engine}_table" "$db" "$table")" "$("${engine}_schema" "$db" "$table")" >>"$file"
  done
}
settings_check() {
  local engine="$1" db="$2" file="$3" table rows hash schema
  while read -r table rows hash schema; do
    note "  $table: $rows rows, contents sha256 ${hash:0:16}, schema ${schema}"
    check "$table row count unchanged" "$rows" "$("${engine}_count" "$db" "$table")"
    check "$table contents unchanged" "$hash" "$("${engine}_table" "$db" "$table")"
    check "$table schema unchanged" "$schema" "$("${engine}_schema" "$db" "$table")"
  done <"$file"
}
new_columns_check() {
  check "user_push_subscriptions has vapid_key_id, failing_since, and last_failure_at" "$push_new_columns" \
    "$("${1}_columns" "$2" user_push_subscriptions | grep -xE 'vapid_key_id|failing_since|last_failure_at' | tr '\n' ' ' | sed 's/ $//')"
}

# from_release: the release's database, upgraded by head.
from_release() {
  local engine="$1" db="$2" url="$3"
  settings_record "$engine" "$db" "$work/$engine-settings"
  evidence base "$url" digest "$work/$engine-base.json"
  migrate_with head "$engine" "$db" "$url" head check
  settings_check "$engine" "$db" "$work/$engine-settings"
  check "user_push_subscriptions exists and is empty" "0" "$("${engine}_count" "$db" user_push_subscriptions)"
  new_columns_check "$engine" "$db"
  evidence head "$url" digest "$work/$engine-head.json"
  note "  $base recipient selection before upgrade: $(tr -d '\n ' <"$work/$engine-base.json")"
  note "  head recipient selection after upgrade:   $(tr -d '\n ' <"$work/$engine-head.json")"
  check "Pushover recipients identical before and after, web push off" "$(digest_of "$work/$engine-base.json")" "$(digest_of "$work/$engine-head.json")"
  if [[ "$engine" == sqlite ]]; then
    sqlite3 "$db" ".schema user_push_subscriptions" >"$out/$engine-user_push_subscriptions.schema"
  else
    psql "$db" -qAtc "SELECT column_name || ' ' || data_type || ' ' || is_nullable || ' ' || coalesce(column_default, '') FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'user_push_subscriptions' ORDER BY ordinal_position" >"$out/$engine-user_push_subscriptions.schema"
  fi
}

# from_previous: a database the previous head migrated, holding a device of
# every kind, upgraded by head and then swept by head's server.
from_previous() {
  local engine="$1" db="$2" url="$3"
  evidence prev "$url" pushseed
  local rows all kept
  rows="$("${engine}_count" "$db" user_push_subscriptions)"
  all="$(push_rows "$engine" "$db")"
  # shellcheck disable=SC2086
  kept="$(push_rows "$engine" "$db" $push_kept)"
  note "  devices registered through $previous's store: $rows ($(push_labels "$engine" "$db"))"
  note "  old columns of every device: sha256 ${all:0:16}"
  settings_record "$engine" "$db" "$work/$engine-prev-settings"
  evidence prev "$url" pushdigest "$work/$engine-prev-push.json"
  migrate_with head "$engine" "$db" "$url" head check
  settings_check "$engine" "$db" "$work/$engine-prev-settings"
  new_columns_check "$engine" "$db"
  check "every device kept" "$rows" "$("${engine}_count" "$db" user_push_subscriptions)"
  check "every device's old columns unchanged, byte for byte" "$all" "$(push_rows "$engine" "$db")"
  check "every device reads vapid_key_id '', failing_since NULL, last_failure_at NULL" "$rows" "$(push_defaults "$engine" "$db")"
  if [[ "$engine" == sqlite ]]; then
    sqlite3 "$db" ".schema user_push_subscriptions" >"$out/$engine-from-previous-user_push_subscriptions.schema"
  else
    psql "$db" -qAtc "SELECT column_name || ' ' || data_type || ' ' || is_nullable || ' ' || coalesce(column_default, '') FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'user_push_subscriptions' ORDER BY ordinal_position" >"$out/$engine-from-previous-user_push_subscriptions.schema"
  fi

  note "  head's server started on the upgraded database with a VAPID key:"
  "$work/clickclack-head" admin webpush keygen >"$work/vapid.env"
  mkdir -p "$work/$engine-serve"
  (
    set -a
    # shellcheck disable=SC1091
    . "$work/vapid.env"
    set +a
    export CLICKCLACK_WEBPUSH_SUBJECT="mailto:evidence@clickclack.test"
    exec "$work/clickclack-head" serve --addr 127.0.0.1:0 --data "$work/$engine-serve" --db "$url"
  ) >"$work/$engine-serve.log" 2>&1 &
  server_pid=$!
  local waited=0
  until grep -q 'web push pruned\|web push prune failed' "$work/$engine-serve.log" || ! kill -0 "$server_pid" 2>/dev/null || [[ "$waited" -ge 300 ]]; do
    sleep 0.1
    waited=$((waited + 1))
  done
  until grep -q 'ClickClack listening' "$work/$engine-serve.log" || ! kill -0 "$server_pid" 2>/dev/null || [[ "$waited" -ge 300 ]]; do
    sleep 0.1
    waited=$((waited + 1))
  done
  kill -TERM "$server_pid" 2>/dev/null || true
  # Bounded like every other wait here: a server that ignores TERM or stalls
  # in its drain is killed after ten seconds instead of hanging the run.
  local stopping=0
  while kill -0 "$server_pid" 2>/dev/null && [[ "$stopping" -lt 100 ]]; do
    sleep 0.1
    stopping=$((stopping + 1))
  done
  if kill -0 "$server_pid" 2>/dev/null; then
    kill -KILL "$server_pid" 2>/dev/null || true
  fi
  wait "$server_pid" 2>/dev/null || true
  server_pid=""
  local leaked=0 value
  while IFS='=' read -r _ value; do
    if [[ -n "$value" ]] && grep -qF -- "$value" "$work/$engine-serve.log"; then leaked=1; fi
  done <"$work/vapid.env"
  if grep -qF 'upgrade-evidence.invalid' "$work/$engine-serve.log"; then leaked=1; fi
  rm -f "$work/vapid.env"
  check "the server log names no endpoint and no VAPID key" "0" "$leaked"
  if [[ "$leaked" == 0 ]]; then
    cp "$work/$engine-serve.log" "$out/$engine-server-sweep.log"
  fi
  note "    $(sed -n 's/.*\(web push enabled: .*\)/\1/p' "$work/$engine-serve.log" | head -1)"
  local pruned
  pruned="$(sed -n 's/.*\(web push prune.*\)/\1/p' "$work/$engine-serve.log" | head -1)"
  note "    ${pruned:-no web push prune line}"
  check "the start sweep removed the three devices whose session ended, and nothing else" \
    "web push pruned 3 devices: 0 refused by their push service for a week, 0 under a retired key, 3 whose session ended" "$pruned"
  note "  devices after the sweep: $(push_labels "$engine" "$db")"
  check "the devices kept are the healthy, development, and previously failing ones" "$push_kept" "$(push_labels "$engine" "$db")"
  # shellcheck disable=SC2086
  check "the devices kept are unchanged in their old columns" "$kept" "$(push_rows "$engine" "$db" $push_kept)"
  check "the devices kept still read an empty key id and no failure times" "4" "$(push_defaults "$engine" "$db")"
  evidence head "$url" pushdigest "$work/$engine-head-push.json"
  note "  $previous web push recipient selection before the upgrade: $(tr -d '\n ' <"$work/$engine-prev-push.json")"
  note "  head web push recipient selection after the sweep:        $(tr -d '\n ' <"$work/$engine-head-push.json")"
  check "web push recipients identical before the upgrade and after the sweep" \
    "$(digest_of "$work/$engine-prev-push.json")" "$(digest_of "$work/$engine-head-push.json")"
  local kind count
  for kind in healthy development failing-old; do
    count="$(selected_of "$work/$engine-head-push.json" "$kind")"
    check "a message selects the $kind device" "yes" "$([[ "${count:-0}" -gt 0 ]] && echo yes || echo "no ($count)")"
  done
  count="$(selected_of "$work/$engine-head-push.json" failing-recent)"
  check "the failing-recent device is held by its backoff, as before the upgrade" "0" "$count"
}

sqlite_copy() {
  local dir="$1"
  mkdir -p "$dir"
  cp "$work/sqlite.orig" "$dir/clickclack.db"
}

note ""
note "From $base, SQLite: copy of $(basename "$snapshot")"
mkdir -p "$work/sqlite"
cp "$snapshot" "$work/sqlite/clickclack.db"
# A checkpointed snapshot has an empty write-ahead log; one that is not needs
# its log and index copied with it.
if [[ -s "$snapshot-wal" ]]; then
  cp "$snapshot-wal" "$work/sqlite/clickclack.db-wal"
  if [[ -e "$snapshot-shm" ]]; then cp "$snapshot-shm" "$work/sqlite/clickclack.db-shm"; fi
fi
sqlite3 "$work/sqlite/clickclack.db" ".backup '$work/sqlite.orig'"
note "  users: $(sqlite_count "$work/sqlite/clickclack.db" users), messages: $(sqlite_count "$work/sqlite/clickclack.db" messages), channels: $(sqlite_count "$work/sqlite/clickclack.db" channels)"
recorded_migrations sqlite "$work/sqlite/clickclack.db" >"$work/snapshot-migrations"
local_only="$(git -C "$repo" ls-tree --name-only "$base_rev:apps/api/internal/store/sqlite/migrations" | sort | comm -13 - "$work/snapshot-migrations" | grep '\.sql$' | tr '\n' ' ' | sed 's/ $//' || true)"
if [[ -n "$local_only" ]]; then
  note "  migrations recorded that $base does not ship: $local_only"
fi
from_release sqlite "$work/sqlite/clickclack.db" "sqlite://$work/sqlite/clickclack.db"

note ""
note "From $base, SQLite: a second copy with Pushover turned on for every user by $base before the upgrade"
note "  (the snapshot's own users never set Pushover up, so the first pass selects nobody)"
sqlite_copy "$work/sqlite-pushover"
evidence base "sqlite://$work/sqlite-pushover/clickclack.db" pushover
from_release sqlite "$work/sqlite-pushover/clickclack.db" "sqlite://$work/sqlite-pushover/clickclack.db"

pg_url_for() { printf '%s' "$admin_dsn" | sed -E "s#(postgres(ql)?://[^/]*/)[^?]*#\\1$1#"; }
pg_fresh() {
  psql "$admin_dsn" -qAtc "CREATE DATABASE $1" >/dev/null
  pg_dbs+=("$1")
  local url
  url="$(pg_url_for "$1")"
  "$work/clickclack-base" migrate --db "$url" >/dev/null
  evidence base "$url" seed
}

note ""
note "From $base, PostgreSQL: fresh database migrated and seeded by $base"
pg_db="clickclack_upgrade_$$"
pg_fresh "$pg_db"
pg_url="$(pg_url_for "$pg_db")"
note "  users: $(pg_count "$pg_url" users), messages: $(pg_count "$pg_url" messages), channels: $(pg_count "$pg_url" channels)"
from_release pg "$pg_url" "$pg_url"

note ""
note "From $previous, SQLite: another copy of $(basename "$snapshot"), migrated by $previous"
sqlite_copy "$work/sqlite-previous"
migrate_with prev sqlite "$work/sqlite-previous/clickclack.db" "sqlite://$work/sqlite-previous/clickclack.db" "$previous"
from_previous sqlite "$work/sqlite-previous/clickclack.db" "sqlite://$work/sqlite-previous/clickclack.db"

note ""
note "From $previous, PostgreSQL: fresh database migrated and seeded by $base, then migrated by $previous"
pg_prev_db="clickclack_upgrade_prev_$$"
pg_fresh "$pg_prev_db"
pg_prev_url="$(pg_url_for "$pg_prev_db")"
migrate_with prev pg "$pg_prev_url" "$pg_prev_url" "$previous"
from_previous pg "$pg_prev_url" "$pg_prev_url"

note ""
if [[ "$failures" == 0 ]]; then note "RESULT: PASS"; else note "RESULT: FAIL ($failures)"; fi
exit $((failures > 0))
