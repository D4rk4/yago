# 0052. Persist crawler frontier checkpoints with bbolt

Date: 2026-07-16

## Status

Accepted

## Context

A crawl order was durable in the node broker, but the crawler frontier existed only in process memory.
Restarting the crawler therefore lost its exact visited set, host state, admission order, and unfinished
pages. Redelivering the broker order could repeat a complete traversal and could give repeated ingest work
a new observation identity.

The BUbiNG architecture described in [arXiv:1601.06919](https://arxiv.org/abs/1601.06919) keeps exact
URL admission on disk and bounds the hot scheduling state in memory. Its disk-backed sieve and per-host
queues show why a crawler that must resume exactly cannot substitute a probabilistic duplicate filter or
an occasional lossy snapshot for the durable frontier. Yago needs the same recovery property while
retaining its existing crawl-policy and scheduling boundaries.

The crawler needs an embedded transactional store with ordered prefix scans, atomic admission and
completion, an exclusive process lock, and no external service. The dependency must work identically in
Docker, systemd, and Debian package installations.

The node-side crawl order, lease, settlement, control, and terminal-run buckets historically shared the
sharded node vault. One broker transition can update several of those buckets, but a transition routed
across independent shards cannot commit as one database transaction. Crawler control state also should
not compete with search and document data for the main vault's quota, eviction, and compaction policy.

## Decision

Use `go.etcd.io/bbolt` version `v1.4.3`, licensed under the MIT license, for crawler frontier checkpoints.
The crawler records a schema version and normalized rows for run definitions, exact visited URLs,
ordered outstanding pages, URL-to-order positions, host state, global host pace, the complete seed
manifest, and terminal settlement outbox. Composite keys escape zero bytes and end broker provenance
with an unambiguous marker. The order identity is the SHA-256 value of the exact broker payload. A
provenance already bound to another order identity, priority, or live lease is rejected.

When the node crawl runtime is enabled, it stores its broker queue, lease and settlement state, controls,
and terminal-run delivery state in `${YAGO_DATA_DIR}/crawlbroker.db`. This dedicated bbolt file provides
one atomic transaction boundary for crawler control state. It is separate from the sharded main vault,
so `YAGO_STORAGE_QUOTA`, main-vault eviction, and main-vault compaction do not limit or reclaim it. There
is a separate soft physical admission boundary for this file
(`crawler.node_state_max_bytes` / `YAGO_CRAWLER_NODE_STATE_MAX_BYTES`) and for
the crawler's `crawler/frontier-v1.db`
(`crawler.frontier_state_max_bytes` / `YAGO_CRAWLER_FRONTIER_STATE_MAX_BYTES`);
each defaults to 4 GiB and `0` disables it. The node rejects fresh order
enqueue at its boundary while migration, ingest, lifecycle, recovery, and settlement continue. The
crawler waits before expanding a fresh order and refuses new discovered-link batches at its boundary;
already committed seed manifests, queued work, recovery, lifecycle, and settlement continue.

At startup, an enabled boundary triggers compaction when the physical file size is equal to or greater
than the configured value. Each process first takes a persistent path-stable sidecar lease and holds it
through stale-copy cleanup, compaction, replacement, directory sync, inspection, and successful exclusive
open of the authoritative database inode. Inside the shared serialized storage-maintenance gate,
compaction remeasures the actual source size, reserves that temporary headroom, reads the authoritative
database read-only, writes a private-mode sibling copy, then syncs and closes it. Still under the startup
lease, it atomically replaces the original and syncs the containing directory. A pre-install failure
leaves the original authoritative and is a recoverable warning; a post-install directory-sync or
inspection failure is reported as an installed durability warning. Lease acquisition or release failure
stops startup. The boundary limits selected new admissions and is not a filesystem quota.

The first startup with the dedicated file performs one retained version-1 migration before listeners
open. The version-1 bucket list is frozen. The migration copies the legacy main-vault rows in ordered
pages of at most 256, commits each page and its target cursor together, fingerprints every source and
target bucket, then commits a marker bound to the migration version and bucket list. An interrupted copy
resumes from the committed target cursor. A conflicting target row, fingerprint mismatch, or marker
mismatch fails startup. The source rows are never changed or deleted, and a future bucket set requires a
new migration version rather than silently extending version 1.

Acquiring the path-stable startup sidecar lease and opening `crawlbroker.db` each wait at most five
seconds. Another process using the same node data directory therefore causes bounded startup failure
instead of an indefinite wait, including across an atomic database replacement.
Prometheus exposes live database use as `crawl_broker_state_used_bytes` and the allocated file size as
`crawl_broker_state_file_bytes`; the latter may remain above live use after rows are deleted.

Seed expansion first publishes the complete ordered manifest in transactions of at most 256 rows. Only
after publication may a durable cursor admit manifest pages into the frontier, advancing in the same
transaction as each admitted batch. An interrupted publication is discarded on open; an interrupted
admission resumes from the stored cursor without fetching or reparsing the mutable seed source. The
manifest remains a lazy producer after restart and shares a 256-page live window with recovered
outstanding pages.

Admission commits the visited marker, outstanding page, page position, host total, and run totals in one
transaction. The page retains its exact observation identity and time, fetch policy, source modification
time, and redirect reservation. A fetch claim is only a bounded in-memory scheduling view; the page row
remains outstanding until page tally, host outcome, global pace, and exact page removal commit together.
Discovered children commit before their parent. A crash therefore replays only work whose complete
outcome was not committed, and replay keeps the original ingest observation identity.

Normal bbolt restoration reads the run header and no more than 256 outstanding rows. Its recovery upper
bound starts at the last committed sequence and extends by the exact committed admission count, so the
cursor never reads an uncommitted page and every later discovered page enters the same bounded refill
path. When the live recovered set drops below 128 pages, the crawler reads at most `256 - live`
additional rows outside the global frontier mutex. Exact visited, retired-host, host-total, and run-total
state for each new candidate batch comes from one checkpoint transaction. Accepted pages advance only
scalar in-memory totals until the bbolt admission commits; they do not enter the visited, ready, pending,
redirect, or host maps directly. The completion tracker receives the full durable pending total,
including pages not yet loaded.

Cancellation is a persisted run transition. Queued pages and an unfinished seed manifest are removed in
transactions of at most 256 rows while in-flight pages retain ownership until their outcome arrives. Host
retirement uses a persisted sequence cursor and examines at most 256 page rows per write transaction.
Both transitions resume during open, isolate other runs and hosts, and update `Pending` in the same
transaction as each page removal. Run and consumed-manifest deletion use the same bounded pattern.

The checkpoint generates one worker suffix once and persists it, producing a stable worker identity per data directory.
Every process also creates a new session identity. Node leases bind worker, session, and run mutations;
an exact replacement lease can atomically rebind an unfinished run while stale fetch completions remain
fenced. Active order leases are independently capped at 1,024 and are not page-fetch workers.

Terminal state is staged in a durable outbox with the exact lease, order identity, session, absolute
tally, rate, and disposition. Delivery is bounded to 64 pending rows and four workers. The node records
the terminal snapshot idempotently and returns an HMAC confirmation token; only a token-confirmed
settlement deletes the outbox and checkpoint. Before token issuance an awaiting row may adopt a new
process session only for the same worker and confirmed live lease. A token-bearing definition is
immutable. A replacement lease arriving after the in-memory run drains recovers this rich settlement
instead of falling back to a legacy acknowledgement.

The node indexes a rich settlement for a fixed 24-hour confirmation window only after terminal progress
delivery is confirmed and an acknowledged run's control completion is durable. Expiry removes the
settlement in a bounded transaction and atomically applies a still-pending requeue. A valid confirmation
that arrives after expiry succeeds idempotently, while an incomplete delivery phase remains retained and
unindexed so cleanup cannot overtake it.

The database and persistent startup sidecar files use mode `0600`, their immediate parent uses `0700`,
opening takes an exclusive bounded lock, and synchronous durability remains enabled. The sidecar is never
unlinked during normal operation, so its inode remains a stable interprocess rendezvous across database
replacement. The schema rejects malformed and future versions rather than guessing a migration. Startup
completes interrupted deletion, seed-manifest, cancellation, host-retirement, and terminal-outbox
transitions before accepting normal work.

## Considered alternatives

Periodic JSON or gob snapshots were rejected because a crash between frontier mutation and snapshot can
repeat or lose work, and rewriting the whole frontier makes checkpoint cost grow with the crawl. An
append-only custom log was rejected because Yago would have to own transaction recovery, compaction,
index rebuilding, locking, and corruption rules. SQLite was considered, but the required operations are
ordered key/value rows and prefix scans; SQL adds a separate schema and driver boundary without a
relational query requirement. Pebble and Badger were rejected for this first crawler adapter because
their compaction and write-amplification lifecycle adds tuning that is unnecessary for the bounded
single-writer checkpoint workload. A Bloom-filter-only visited set was rejected because false positives
would permanently omit URLs after restart.

## Consequences

The checkpoint recovers exact visited membership, seed suffix, admission order, host availability and
pace, absolute tallies, controls, redirects, and unfinished work without replaying completed pages. The
resident scheduler retains only its bounded queued, ready, and in-flight window; completing,
cancelling, or retiring those pages evicts their visited, redirect, and host cache entries because bbolt
remains authoritative. A bbolt database permits one writer transaction at a time, so all potentially
large transitions are cursor-driven bounded writes; unrelated runs can acquire the writer between
chunks. Concurrent reads continue while no writer holds the transaction. Concurrent hot-path writes use
bbolt group commit with a 2 ms delay and 256-operation cap, preserving synchronous durability without
one separate commit per crawler goroutine. A second process cannot open the same file and fails after the
bounded lock timeout.

After the version-1 completion marker is committed, `crawlbroker.db` is authoritative and later current
starts do not consult the retained source rows. Those rows remain a stale cutover copy in the main vault
and continue to consume its quota. Deleting only `crawlbroker.db` makes a later current start migrate that
stale copy again; starting an older node directly against the upgraded data tree makes it use the same
stale legacy rows. Either action can resurrect work already settled after cutover. Rollback therefore
requires the matching older binaries and one coordinated stopped backup of the complete node and crawler
data, not removal of the dedicated file or a package downgrade in place.

The retained migration copies the legacy bytes faithfully but cannot reconstruct a transition that an
older sharded node failed to commit consistently. Specific legacy indexes may still receive their normal
startup reconciliation, but the migration makes no general repair claim. Atomic cross-bucket broker
transitions are guaranteed for mutations committed in the dedicated database, not retroactively for
historical legacy state.

Initial seed expansion and manifest encoding remain proportional to the caller-supplied seed list.
Checkpoint open also completes any interrupted deletion, cancellation, host-retirement, or manifest
transition synchronously through bounded transactions. Operator cancellation walks every cleanup chunk
before returning so its terminal settlement remains immediate; it is memory-bounded but proportional to
the cancelled run. Production bbolt recovery uses the bounded interface; an alternate checkpoint
implementation that supplies only the legacy load method does not receive this memory guarantee.

Graceful shutdown detaches queued work without deleting it, waits within the configured grace period for
in-flight work, and reconciles terminal settlements. SIGKILL relies only on committed database state and
same-worker adoption. Session-aware leases keep that stable-worker affinity after their deadline so a
different data directory cannot restart the traversal; deferred and legacy sessionless leases retain
ordinary expiry and requeue. Heartbeat calls are bounded to one second; an omitted, expired, or otherwise
lost active grant cancels the current order stream so its same-worker reconnect adopts the parked lease.
A worker without a confirmed grant parks on grant or frontier generation notifications instead of
polling through `Take` and `Abandon`.

The file lives at `crawler/frontier-v1.db` under the operator-selected `YAGO_DATA_DIR`. Docker uses a
crawler-owned volume because node and crawler images use different unprivileged identities; bare-metal
and package services share the operator-owned data tree. A checkpoint is host-local: moving an active
lease to another crawler host requires transferring or sharing that data directory.
