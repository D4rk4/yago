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
thrashing. It does **not** shrink the files on disk; that is slice C.

### B. Evict the document alongside the rest of a URL

`purgeURLs` also drops the URL's `documents` entry, so both the quota sweep and
the on-demand/ tombstone `Evictor` remove the whole URL, not just its index side.

The key-shape mismatch is the crux. Postings, word-references, and url-metadata
are keyed by the **URL hash**; documents are keyed by the **normalized URL
string**. `purgeURLs` starts from hashes (the quota sweep selects stale URLs by
hash; the tombstone path hashes its `SourceURL`). The bridge, confirmed by
tracing the ingest:

- The crawler builds the document and the url-metadata row from the same
  `page.URL`. The document's `NormalizedURL` **is** `page.URL`
  (`yagocrawler/internal/pageindex`); the url-metadata row stores
  `Properties["url"] = EncodeBase64WireForm(page.URL)` and is keyed by
  `HashURL(page.URL)`.
- Therefore `DecodeWireForm(row.Properties["url"])` is a **byte-for-byte match**
  for the `documents` key, and the row's hash equals `HashURL(NormalizedURL)`.
  (This URL↔hash identity is already relied on in
  `node_admin_index_delete.go`, which deletes a document by URL then evicts by
  `HashURL(url)`.)

So `purgeURLs` resolves each hash to its stored URL via
`urlmeta.URLDirectory.RowsByHash` + `DecodeWireForm`, then deletes the document
by that URL. Two ordering/robustness rules:

- **Resolve and delete the document *before* purging the url-metadata rows.**
  Once the metadata row is gone the URL can no longer be resolved from a hash, so
  a crash after the metadata purge but before the document delete would orphan
  the document forever. Deleting the document first makes a mid-purge crash
  self-heal: the still-present metadata lets the next sweep re-resolve and retry.
  Full cross-store atomicity is neither available (the vault relaxes cross-shard
  atomicity by design, ADR-0025) nor assumed elsewhere — `deleteOne` in the
  admin delete path already deletes across the index, documents, and evictor as
  separate steps.
- A resolved hash may legitimately have **no** document: the quality gate can
  store a url-metadata row and postings without a document.
  `Delete` returning `(false, nil)` is the intended idempotent no-op.

The recrawl tombstone path routes through the same `purgeURLs`, so it gains
document eviction with no special-casing, and resolving the exact stored URL
(rather than re-using the tombstone's `SourceURL`) is robust against recrawl
spelling drift, since `HashURL` collapses spellings the exact document key does
not.

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
- Eviction becomes effective: it can drop below the high-water mark and it now
  reclaims the dominant consumer (documents), so a node under quota pressure
  bounds its storage instead of thrashing postings to the floor.
- Compaction periodically quiesces the engine per over-full shard. This is a
  brief, bounded, once-per-interval stall, off by setting `off`.
- A dead-page tombstone now removes the document too, closing the ADR-0034
  orphan.
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
  of the existing document corpus for no functional gain. Rejected; the
  `RowsByHash` + `DecodeWireForm` resolve is exact and cheap for bounded batches.
- **Counting free pages in `UsedBytes` (status quo) and only compacting.**
  Leaves the metric lying between compactions and keeps the eviction sweep
  thrashing. Rejected; truthful accounting is the core fix.
