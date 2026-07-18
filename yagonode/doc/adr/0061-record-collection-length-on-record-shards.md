# 0061. Record collection length changes on record shards

Date: 2026-07-18

## Status

Accepted

## Context

Each collection kept one exact length value in `__lengths__`, keyed by the
collection name. A mutation therefore touched both the record shard and the
single shard selected for that length key. Concurrent crawler or YaCy transfer
writes to different records in the same collection still serialized at that
second shard, defeating the shard-vault writer concurrency introduced by
ADR-0037.

bbolt permits only one read-write transaction per database file and recommends
avoiding very large random-write transactions. The crawler could also combine
up to sixteen deliveries of 8,192 postings into one storage call. Those two
properties amplified admission waits on a busy node even when the records were
distributed across independent shard files.

References:

- [bbolt transactions](https://github.com/etcd-io/bbolt#transactions)
- [bbolt caveats for large transactions](https://github.com/etcd-io/bbolt#caveats--limitations)
- [Staged Event-Driven Architecture](https://www.usenix.org/legacy/events/sosp01/full_papers/welsh/welsh.pdf)

## Decision

Preserve the existing `__lengths__` value as an immutable upgrade base. New
shard-vault inserts and removals record monotonic addition and removal totals in
`__collection_length_changes__` on the same physical shard as the mutated
record. The record and its matching length change commit in the same bbolt
transaction. `Len` returns the legacy base plus every shard's additions minus
every shard's removals.

Historical change rows remain pinned to the shard where they were written.
Linear-hash splitting moves ordinary records but neither copies nor deletes
these rows, preventing a stale source and copied target from both contributing
to the aggregate. A new mutation after a split records its change on the
record's current shard.

The generic vault engines retain the existing single length value. Existing
shard vaults open without scanning or rewriting stored collections. A current
binary can therefore upgrade in place, but an older binary opened after new
mutations cannot include the new shard-local changes and is not a supported
downgrade path.

Crawler ingest sends no more than one 8,192-posting delivery to a storage
transaction. A grouped delivery remains retryable as one idempotent group if a
later chunk encounters backpressure or an error.

## Consequences

- Independent writes to the same collection can overlap when their records map
  to different shards.
- Collection length remains exact across restart, partial multi-shard commit
  retry, and linear-hash split.
- `Len` reads one exact counter row from every current shard. This cost is
  linear in shard count and is accepted for bounded statistics and Admin paths;
  repeated eight-shard measurements are approximately 4–5 microseconds per
  call.
- No new dependency, listener, service, setting, or deployment-specific
  behavior is introduced.
- Rollback requires a coordinated backup made before the first new mutation;
  copying only the executable to an older version is not supported.

## Alternatives considered

### Keep one counter and serialize same-collection writes

Rejected. Documents, URL metadata, and postings are the dominant concurrent
ingest collections, so this retains the measured bottleneck.

### Recount every collection during upgrade

Rejected. A full scan makes startup proportional to the stored corpus and is
unnecessary when the existing exact value can remain the base.

### Use an asynchronous approximate counter

Rejected. Collection lengths participate in storage, protocol, and operator
reports that already promise exact committed state.

### Add a separate ingress journal

Rejected for the current workload. Bounded transactions, independent shard
writes, protocol-level overload responses, and sender retry preserve the
durable-acceptance contract without another recovery format. Reconsider this
only if post-change measurements still show sustained overload.
