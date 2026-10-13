# Web push evidence

Repeatable checks for the web push authorization and upgrade paths, run
against built binaries rather than unit fixtures. Neither writes message text,
emails, keys, tokens, or endpoints to its output.

## Send-time authorization

```sh
node scripts/web-push-evidence/authorization.mjs --out <dir>
```

Builds the server with `-tags clickclack_e2e_unsafe_callbacks` (the tag only
lets devices point at a loopback push service), serves it with development
authentication off, and signs users in through magic links minted by the admin
CLI. For each case it holds all four delivery workers on a fake push service,
posts a message so the recipient's push waits in the queue, then:

| case | what changes while the push waits |
| --- | --- |
| `control` | nothing |
| `session-revoked` | the recipient signs out |
| `subscription-deleted` | the recipient removes the device |
| `membership-removed` | the recipient's workspace membership row is deleted |
| `key-retired` | the recipient's device row is moved to a key the server does not sign with, as a rotation leaves it |

and releases the workers. The recipient's device must receive one push in the
control case and none in the others, each with a `web push delivery skipped`
line naming the reason. After `key-retired`, `GET /api/me/push` naming the
device must answer `this_device_stale: true`, and a message posted with the
workers free must queue nothing for it: no push and no log line.

A last case is one browser shared by two accounts, signed in by cookie the
way a browser is. Signed in as A, the device registers for A; the same jar then
signs in as B, and a registration that still names A, as a tab left open on A
asks after a renewal, must answer `409`. B must list no device, a message to B
must reach no endpoint, and a message to A must still reach A's device.

`summary.txt` holds the tables; `server-webpush.log`
and `server.log` hold what the server printed. The run fails if a session
token, a sign-in token, or an endpoint appears in the server's output.
Requires `go`, `node`, and `sqlite3`.

## Upgrade from a release and from the previous head

```sh
scripts/web-push-evidence/upgrade.sh \
  --sqlite-snapshot <existing.db> \
  --postgres-admin-dsn "$PGADMIN_DSN" \
  --out <dir> [--base v0.5.1] [--previous 4fdae122]
```

`PGADMIN_DSN` holds a PostgreSQL connection URL for a role that may create
and drop databases, naming any existing database on the server. The run
creates and drops its throwaway databases through it, and reaches each by
swapping the database name in the URL.

Exports the release tag, the pull request's previous head (the last one
published before the dead-device sweep, with the first push migration and not
the second), and `HEAD` with `git archive`, and builds all three. The previous
head must be reachable in the clone; after a squash merge it is not, so pass
`--previous` with any revision that holds the first push migration and not the
second. It copies
`upgradeevidence/evidence_test.go` into each tree, and
`upgradeevidence/push_test.go` into the two that have push subscriptions, so
the same code questions every version. The snapshot is only read; every pass
works on a copy.

From the release, the run upgrades a copy of the snapshot with the head
binary's `migrate`, then a second copy after the release has turned Pushover
on for every user, so a database whose users never set up Pushover still
exercises recipient selection. On PostgreSQL it creates a throwaway database,
migrates and seeds it with the release, and upgrades it with head. Each pass
must show head applying every migration the database lacked and nothing else
(the two push migrations on each engine); `user_notification_settings` and
`channel_notification_settings` unchanged in row count, contents hash, and
schema; `user_push_subscriptions` created, empty, and holding
`vapid_key_id`, `failing_since`, and `last_failure_at`; and the same digest of
Pushover recipient selection (every message, with no mentions and with every
user mentioned, filtered by message access the way the dispatcher does) from
the release before the upgrade and from head after it, with web push off.

From the previous head, the run migrates another copy of the snapshot, and a
second PostgreSQL database seeded by the release, with the previous head, and
registers through that head's store one device of each kind the sweep must
judge: one on a live session that has delivered, one whose session was
revoked, one whose session expired, one whose session row is gone, a
development device with no session, one refused three times and backing off,
and one refused nine times and quiet for forty days. Upgraded by head, only
the second push migration may apply, every device must keep its old columns
byte for byte and read `vapid_key_id = ''`, `failing_since` NULL, and
`last_failure_at` NULL, and both notification settings tables must be
unchanged. Head's server then starts on that database with a fresh VAPID key.
Its start sweep must log removing exactly the three devices whose session
ended, and the four others must remain unchanged: a device with no recorded
key is never under a retired key, and one with no recorded refusal time is
never refused for a week. Web push recipient selection replayed by head after
the sweep must match the previous head's before the upgrade, with the healthy,
development, and long-quiet devices selected and the backing-off one held.

`summary.txt` states the three commits and every check, `ok` or `FAIL`, and
ends `RESULT: PASS` or `RESULT: FAIL`. The server's output for each sweep is
kept as `<engine>-server-sweep.log` when it names no endpoint and no VAPID
key. Requires `git`, `go`, `sqlite3`, `psql`, and `shasum`.

## First activation and key rotation on a real device

A headless browser will not issue a real push subscription, so these two need
a phone. The server logs make each step checkable.

1. Start the server with a VAPID pair and note the startup line
   `web push enabled: application server key <fingerprint>`.
2. On the phone, remove any existing home-screen install of the server, open
   it in the browser, add it to the Home Screen, open it from there, and sign
   in.
3. Open account settings, then Notifications, turn on
   "Push notifications on this device", and allow notifications. The switch
   must turn on at the first attempt, and the log must show
   `web push device registered for user <id> via <push service host>`.
4. From another account, post a message the user can read, with the app
   closed. The notification must arrive, and the log must show
   `web push delivered for user <id>`.
5. Rotate the pair: generate one with `clickclack admin webpush keygen`
   written straight into the server's configuration, and restart. The startup
   line must show a new fingerprint.
6. With the app still closed, post again. Nothing arrives: the phone's
   subscription belongs to the old key. The log shows the push service
   refusing it, as a `web push delivery failed` line or a
   `reports it is gone` line for the user.
7. Open the app from the Home Screen. The log must show
   `web push device removed by user <id>` followed by
   `web push device registered for user <id>`: the app found its subscription
   was made under the old key, replaced it, and removed the old row. A
   `web push device refreshed` line instead means the browser did not report
   the subscription's key, and the device kept the dead subscription.
8. Close the app and post again. The notification must arrive, with a
   `web push delivered` line.
