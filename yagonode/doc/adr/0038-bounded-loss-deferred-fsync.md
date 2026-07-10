# 0038. Bounded-loss deferred fsync for the sharded vault (opt-in, restart-required)

Date: 2026-07-10

## Status

Accepted

## Context

The ingest path has been driven toward one durable write per unit of work: a
crawl batch lands through one vault commit per touched shard (IO-AGG-01), the
lease queue checkpoints instead of fsyncing every heartbeat (IO-AGG-02), and a
shard no longer persists its freelist on every commit (IO-AGG-03). What remains
is the fsync inside each surviving commit. bbolt makes a commit durable by
writing the dirty data pages, fsyncing them, then writing and fsyncing the meta
page that points at them — the data-before-meta ordering is what guarantees a
crash never exposes a meta page referencing pages that never reached disk. On
fsync-bound storage (spinning disks, network block volumes, consumer SSDs) that
per-commit fsync is now the dominant write cost, and with the shard pool growing
(ADR-0037) a busy ingest fsyncs across many files.

bbolt already exposes the lever: `DB.NoSync` skips a commit's fsyncs. But NoSync
forfeits the ordering guarantee. On a clean process stop the OS page cache still
carries the writes and flushes them, so nothing is lost; the exposure is a power
loss or kernel panic, after which an affected shard can lose its unsynced writes
and, in the worst case, need bbolt's second meta page to open at the last
consistent state. Our production node runs on XFS, where NoSync is **not**
crash-safe, so this can never be the default and must never silently turn on.

This is a durability/throughput trade the operator must choose deliberately, not
a universal optimization. It also must not be flipped while the node is serving:
toggling NoSync under live writers races the commit path and leaves the store in
an ambiguous durability state for the in-flight transactions.

## Decision

Add deferred fsync as an **opt-in, default-off, restart-required** durability
mode for the sharded vault, with a background pass that bounds the loss window.
Two slices, behind this ADR.

### A. NoSync set once at boot, restart-required

A node setting `storage.defer_fsync` (env `YAGO_STORAGE_DEFER_FSYNC`, default
off) flips `DB.NoSync` on every shard exactly once at boot, before any listener
starts — the engine's `SetDeferredFsync` walks the shards under the shared gate
and records the mode in an `atomic.Bool`. The setting has no live-apply hook, so
a change lands on the next restart, mirroring the perimeter settings that rebuild
their guards at boot. Restart-required is the point, not a limitation: the shards
are reconfigured while single-threaded at startup, so NoSync is never flipped
under a concurrent commit.

On a clean shutdown `Close` fsyncs each shard before closing it when the mode is
on, so a planned stop or a restart-to-change-the-setting loses nothing — only an
abrupt power loss is exposed, and only back to the last background flush.

### B. Staggered background flush, off the hot path

A maintenance loop — modeled on the compaction (ADR-0036 C) and shard-growth
(ADR-0037 C) loops — calls `SyncShards` every `deferredSyncPollInterval`.
`SyncShards` flushes the shards one at a time with a short pause between them, so
the deferred writes reach disk on a bounded cadence without the synchronized
fsync storm that flushing every shard at once would create — the very burst
deferral is meant to avoid. Each shard is flushed while holding the engine's
shared gate so a concurrent split or compaction cannot close it mid-flush; the
pause between shards is taken with the gate released. When the mode is off the
loop reports it disabled and every tick is a no-op, so the loop runs
unconditionally and costs nothing on a default node. The loss window is therefore
bounded to roughly the poll interval plus one sweep.

No wire-format or peer-protocol change — deferred fsync is an internal storage
policy invisible to peers and the YaCy API.

## Consequences

- On fsync-bound storage a node that opts in trades a bounded, operator-accepted
  loss window for markedly fewer fsyncs, so ingest is no longer paced by the
  disk's flush rate.
- The default stays fully crash-safe on every filesystem, including the XFS
  production node — deferred fsync is off unless an operator sets it.
- Restart-required keeps the shards from being reconfigured under load and makes
  the mode a deliberate choice; a clean shutdown flushes, so planned restarts and
  the setting change itself lose nothing.
- The exposure is narrow and named: an abrupt power loss or kernel panic can lose
  writes newer than the last background flush on the affected shards, bounded by
  the flush cadence; a clean process crash does not lose data (the OS page cache
  survives it).
- One more background loop joins compaction and growth; it is a no-op while the
  mode is off, so it adds nothing to a default deployment.

## Alternatives considered

- **Default deferred fsync on.** The largest throughput win, but unsafe on XFS
  and any filesystem without atomic same-file overwrite: a power loss could
  corrupt a shard past bbolt's meta-page fallback. Rejected — durability cannot
  regress silently for every deployment to speed up the ones that accept the risk.
- **Flush every shard at once, periodically.** Simpler than staggering, but
  reintroduces a synchronized fsync burst across the whole pool each interval —
  the exact spike deferral exists to remove. Rejected in favor of spreading the
  flushes across the interval.
- **Live-toggleable mode.** Convenient, but flipping NoSync under live writers
  races the commit path and leaves in-flight transactions in an ambiguous
  durability state. Rejected: the setting is restart-required so the flip happens
  while single-threaded at boot.
- **OS-level durability relaxation** (mount with `barrier=0`, `eatmydata`).
  Achieves the same fsync elision but moves durability policy out of the
  application, applies indiscriminately to unrelated files, and is invisible to
  operators reading the node's settings. Rejected — the trade belongs in the node,
  named and defaulted safe.
- **A redo log / WAL in front of the shards** (WiscKey-style). Would let commits
  defer the fsync while keeping crash recovery, but it is a much larger storage
  change with its own write amplification. Deferred to the performance-survey
  backlog; NoSync with a bounded flush is the cheap, reversible first step.
