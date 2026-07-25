# Managed channel reconciliation proof

This proof exercises the production HTTP handler against the real SQLite store
and separately stresses the store identity constraint with concurrent callers.

## Reproduction

```sh
go test -count=1 -v ./apps/api/internal/httpapi \
  -run 'TestManagedChannelReconciliationBotAuthorization|TestMutationAndEphemeralEndpoints'

go test -count=1 -v ./apps/api/internal/store/sqlite \
  -run TestManagedChannelReconciliationConverges
```

The HTTP scenarios verify:

- the first desired-state request returns `201 created` with a
  `channel.created` event;
- an exact replay returns `200 unchanged`, the same channel ID, and no event;
- changed desired state returns `200 updated` with one `channel.updated` event;
- omitted mutable desired fields reconcile to documented defaults or clearing;
- archived channels can be reopened through reconciliation;
- normal channel updates cannot clear the reconciled identity;
- a bot with `channels:write` can reconcile in its bound workspace; and
- the same bot token is rejected in another workspace, even when the bot is a
  member there.

The store scenario sends six concurrent requests for one provider identity. It
asserts that they converge on one channel, with exactly one `created` result
and five `unchanged` results. It also verifies provider namespace isolation,
archive/reopen behavior, and immutable identity protection.

Postgres runs the equivalent convergence and changed-state scenario whenever
the repository's Postgres test environment is configured.
