# 0036. Truthful storage accounting, document eviction, and periodic compaction

Date: 2026-07-08

## Status

Accepted

## Context

An operator reported a storage anomaly: the node showed only ~3287 RWI words
stored, yet its ~1 GB storage quota read as full. Three independent defects in
the sharded vault (ADR-0025) combine to produce exactly that symptom.

**1. `UsedBytes` measures file size, not live data.** The runtime opens
`shardvault` (`internal/yagonode/main.go` wires `openRuntimeVault =
shardvault.OpenAt`). `shardvault.engine.UsedBytes` summed `os.Stat(shard).Size()`
across the shard files. bbolt never returns freed pages to the OS: a shard file
grows to its high-water mark and stays there. After churn — deletes, recrawl
re-ingest, eviction — the file size overstates the bytes actually in use, often
by a wide margin. So the figure the quota and its eviction sweep read was the
*peak file size*, not live usage. (`boltvault`, used only in tests, already
subtracts free pages in `capacity.go`; `shardvault` never got the same
treatment.)

**2. The `documents` bucket is never evicted.** Every crawled URL stores a full
JSON `Document` — including up to 1 MiB of extracted text plus headings,
out/inlinks, images, and metadata (`internal/documentstore`) — keyed by the
document's normalized URL. The quota sweep's `purgeURLs`
(`internal/eviction/quota_sweeper.go`) drops a URL's postings, word-references,
and url-metadata, but **not** its document. Documents therefore accumulate with
every distinct URL ever crawled, and are the dominant on-disk consumer, while
the eviction that is supposed to bound storage never touches them. This is also
a correctness gap in RECRAWL-02 / ADR-0034: a dead-page tombstone purges the
postings and metadata but leaves the document orphaned.

**3. Eviction cannot get below the high-water mark.** `Sweep` loops while
`UsedBytes >= 0.9 * quota`. Because `UsedBytes` was file size and deletes do not
shrink files, `used` never dropped below the mark. The sweep purged essentially
every evictable posting (leaving only the most-recently re-ingested ~3287) and
*still* saw the store as full, then thrashed. Meanwhile the real consumer — the
documents — sat untouched.

The reported "3287 words, 1 GB full" is the exact signature of these three
acting together: file-size accounting pins usage at the peak, the sweep strips
postings to the floor trying (and failing) to drop below it, and the document
corpus that actually fills the disk is outside the sweep's reach.

## Decision

Fix all three, in three slices, behind this ADR.

### A. `UsedBytes` reports live bytes

`shardvault.engine.UsedBytes` sums, per shard, `tx.Size() - (FreePageN +
PendingPageN) * PageSize` — the in-use bytes, excluding the freelist — inside a
read transaction, mirroring `boltvault`. This alone makes the metric truthful
and lets the eviction sweep observe space that deletes actually free, so it stops
thrashing. Exact measurements also refresh the one-second observation shared by
concurrent admission preflights, so an eviction pass immediately updates their
capacity view without turning admin or eviction reads into cached estimates. It
does **not** shrink the files on disk; that is slice C.

### B. Evict one complete source lineage

Documents use normalized URL keys, while RWI postings, URL references, and URL
metadata use the YaCy URL hash. Deletion therefore needs both identities when
they are known; a hash alone cannot reconstruct an absent document key.

Quota eviction, crawler tombstones, Admin deletion, and the startup redirect
sweep delegate to one full-lineage owner. A known-URL caller passes both the URL
and its hash. Quota eviction begins with a hash and resolves the stored URL from
URL metadata before taking ownership. The compatibility hash-only path still
does that resolution when metadata exists and always removes the addressable
hash-keyed rows, but it cannot guess a document key after metadata is already
missing. A crawler tombstone carries `SourceURL`, so it can delete the complete
lineage even in that state. An unhashable Admin URL delegates once to the
document-lineage owner; there are no addressable RWI or metadata rows to purge.

Live ingest and deletion share the same ordered, per-source lifecycle
reservation. Ingest reserves the batch source URL plus the document's normalized
and canonical source identities after admission and holds them through content
cluster assignment, document and Bleve publication, anchor publication, URL
metadata, stale-RWI cleanup, posting intake, observation completion, and
acknowledgement. Deletion canonicalizes, deduplicates, and sorts its known source
URLs before reserving them. The reservation is scoped to those sources, so
unrelated crawls remain concurrent and a global ingest lock is unnecessary.

The full-lineage owner performs deletion in crash-recoverable order:

1. Begin the durable content-cluster deletion transition and read the affected
   source and survivor projections.
2. Clear the source's outbound-anchor contribution, project and finalize the
   affected inbound anchors, and refresh surviving cluster documents and Bleve
   rows.
3. Delete the source document and Bleve row, then finalize the durable cluster
   transition.
4. While the source reservation is still held, purge RWI postings before URL
   metadata, report the observation outcome, and release the reservation.

The durable cluster transition stays visible to replay until external document
and index projection succeeds; partially projected cluster evidence is hidden
from normal reads. URL metadata remains the last hash-to-URL recovery aid for a
legacy hash-only retry. These rules make a crash or a returned error retryable,
but they do not claim one transaction across the document store, Bleve, content
clusters, anchors, RWI, and URL metadata. Cross-shard vault atomicity remains
relaxed by ADR-0025. Missing documents and repeated deletions remain idempotent
no-ops.

### C. Periodic, configurable compaction returns freed pages to the OS

Slice A stops the false "full" and lets eviction reuse freed pages, but the
shard files stay at their high-water mark. To actually reclaim disk, a
background maintenance pass compacts the shards on a schedule.

- **Primitive.** `shardvault` compacts one shard at a time with `bolt.Compact`
  into a temporary file, then atomically renames it over the shard and reopens
  it. Compaction runs under the engine's exclusive `globalGate` so no read or
  write transaction is in flight on that shard while it is swapped. Today only
  writers coordinate through the gate (readers take no lock); compaction requires
  the engine to be quiescable, so `View` also takes the gate shared, exactly like
  `Update`. Only shards whose free-page ratio exceeds a threshold are compacted,
  so a healthy shard is skipped and the per-cycle stall is bounded to the shards
  that actually need it. The stall is the accepted trade-off for an online
  reclaim; it is documented, runs at most once per configured interval, and is
  acceptable for a P2P search node (not a low-latency OLTP store).
- **Schedule.** A background loop (modeled on the eviction sweep loop) runs the
  compaction pass every configured interval.
- **Setting.** A new node setting `storage.compaction.interval` (env
  `YAGO_STORAGE_COMPACTION_INTERVAL`) controls the cadence, default `1d`, `off`
  disables it. It reuses the recrawl-interval vocabulary (`1d`, `12h`, `off`) for
  a consistent duration UX and applies live (the loop reads the current interval
  each cycle), so it needs no restart. It surfaces in the admin console under a
  new **Storage** Configuration tab (keys prefixed `storage.`).

## Consequences

- The storage metric and the `/metrics` `storage_used_bytes` gauge now report
  live usage; an operator watching them will see usage fall after eviction and
  compaction instead of a stuck peak. Dashboards keyed to the old file-size
  semantics will read lower.
- Eviction becomes effective: it can drop below the high-water mark and one
  owner reclaims the complete source lineage, including the dominant document
  payload, so a node under quota pressure bounds storage instead of thrashing
  postings to the floor.
- Concurrent ingest and deletion serialize only on the affected source
  identities. Admin, redirect, quota, and tombstone callers cannot race separate
  partial deletion sequences or erase a newer ingest tail.
- Compaction periodically quiesces the engine per over-full shard. This is a
  brief, bounded, once-per-interval stall, off by setting `off`.
- A dead-page tombstone carrying URL plus hash removes the complete lineage even
  when URL metadata is already missing, closing the ADR-0034 orphan.
- No wire-format or peer-protocol change. Accounting, eviction scope, and an
  internal maintenance pass only.

## Alternatives considered

- **Start-up-only compaction.** Simpler (no online quiescing) but only reclaims
  on restart; the operator asked for a periodic, configurable cadence, and a
  long-running full node would never reclaim between restarts. Rejected.
- **Per-operation reader locks for online compaction.** Taking a per-shard read
  lock on every `View`/`Get` would let compaction swap a single shard without a
  global quiesce, but it taxes the hot read path for a once-a-day operation. The
  shared `globalGate` on `View` keeps the uncontended cost to one atomic and only
  blocks during the rare swap. Rejected in favor of the gate.
- **Keying documents by URL hash** to remove the resolve step. A data migration
  of the existing document corpus for no functional gain. Rejected; known-URL
  callers already carry the document identity, and the bounded legacy hash-only
  path resolves it through URL metadata when that metadata exists.
- **Counting free pages in `UsedBytes` (status quo) and only compacting.**
  Leaves the metric lying between compactions and keeps the eviction sweep
  thrashing. Rejected; truthful accounting is the core fix.
