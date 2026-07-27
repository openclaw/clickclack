# GitHub project webhook runtime proof

Validated on 2026-07-27 against an existing SQLite deployment upgraded from
commit `2df52a3` to `5a77781`. Repository, project, delivery, and secret values
are redacted below. No webhook secret was printed or exported.

## Existing database upgrade

The database was backed up while the old container was stopped. The upgraded
container started successfully and served both local and public health checks.

```text
GET /healthz
200 {"status":"ok"}

latest migration:
0040_github_delivery_retries.sql

github_deliveries columns:
project_id, delivery_id, event_type, status, created_at,
updated_at, completed_at, failed_at
```

The SQLite and Postgres migration tests also create rows using the prior schema,
apply the retry migration, and verify that processing and completed deliveries
retain their state and timestamps.

## Signature rejection

A real project endpoint received an `issues` payload with an invalid signature
and a delivery ID that had not been used before.

```text
HTTP 401
{"error":"invalid GitHub webhook signature"}

matching github_deliveries rows: 0
```

## Failure recovery

An existing completed test-issue delivery was moved to `failed` in the live
database, then the same payload and delivery ID were replayed with a valid
signature. An online SQLite backup was taken before this controlled state
change.

```text
before replay:
status=failed
completed_at=NULL
failed_at=<timestamp>
issue_thread_count=1

replay:
HTTP 202
{"delivery_id":"<redacted>","status":"accepted","updates":1}

after replay:
status=complete
completed_at=<timestamp>
failed_at=NULL
issue_thread_count=1
```

This proves a handler failure does not permanently suppress a valid GitHub
redelivery and that recovery does not create a second issue root.

## Duplicate suppression

The now-completed delivery was replayed once more with the same valid signature
and delivery ID.

```text
HTTP 202
{"delivery_id":"<redacted>","status":"duplicate"}

issue_thread_count=1
```

## Automated coverage

- SQLite and Postgres: first claim, active-processing suppression, explicit
  failure reclaim, stale-processing reclaim, completion, and duplicate
  suppression.
- SQLite and Postgres: upgrade from the pre-retry delivery schema.
- HTTP handler: injected post-claim storage failure followed by successful
  redelivery of the same delivery ID, with one resulting PR root.
- HTTP handler: invalid-signature rejection and duplicate event suppression.
