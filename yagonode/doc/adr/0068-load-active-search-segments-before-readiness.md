# 0068. Load active search segments before readiness

Date: 2026-08-27

## Status

Accepted; mapping residency amended by
[ADR-0069](0069-populate-active-search-mappings-before-readiness.md)

## Context

Release v0.0.45 reduced each production Scorch shard to one active root before
serving, but its production acceptance still failed five new common searches.
Each request reached the 1.8-second local-search budget with no row, and four
Bleve searches remained inside their process admissions after the HTTP responses
returned. The first burst incurred 387 major faults and about 42 MiB of physical
reads. Later requests were refused while those four searches waited on storage.

Zapx maps each persisted segment but memoizes only field metadata on the heap.
Postings and stored-data pages remain demand-faulted. The existing lexical
warm-up runs one match-all search and opens field dictionaries; it does not
touch every page that an arbitrary first query can need. Bleve's alias also
starts one search for every shard and waits for all of them, so one blocked
page in any shard can retain the complete query after its caller's deadline.

The Linux [readahead manual](https://man7.org/linux/man-pages/man2/readahead.2.html)
describes sequential preloading as useful only when it completes before the
later read and the pages remain resident. The pinned
[zapx implementation](https://github.com/blevesearch/zapx/tree/v17.2.3)
uses file-backed mappings for persisted roots. Cache-aware retrieval results in
[arXiv:2606.21924](https://arxiv.org/abs/2606.21924) also make physical work a
joint property of the index, workload, and cache policy rather than a document
count alone.

A disposable 800,000-document fixture placed ten roots in each of eight shards.
The production constructor consolidated it to eight active roots totaling
491,133,732 bytes. After file sync and cache eviction, the old lexical warm-up
alone produced 192,023 minor faults and failed all 100 synchronized four-CPU
queries at p99 2.523 seconds. Loading every active root before the same burst
reduced that round to 50,598 minor faults and p99 1.873 seconds, but 20 requests
still exceeded 1.8 seconds. Two hundred four-CPU requests reached p99 2.317
seconds with 195 failures; two hundred eight-CPU requests reached p99 1.835
seconds with 119 failures. The remaining limit was scoring CPU, not storage
input. Cache loading can remove cold I/O; it cannot manufacture concurrent
compute capacity.

## Decision

After rebuild and bounded segment consolidation, and before lexical warm-up or
readiness, obtain one referenced Scorch snapshot at a time. Select only the
snapshot's nonempty persisted segment paths. Read those files sequentially to
end through one reusable 4 MiB window, with one snapshot and one file open at a
time. Open each basename within its exact index directory. Check startup
cancellation before each shard, before each file, and between windows.

Keep the snapshot reference until every selected path in that shard is read so
the Scorch purger cannot retire a file during loading. A snapshot acquisition,
open, read, close, cancellation, invalid reader result, or no-progress result
refuses index construction and therefore readiness. Preserve joined close
failures rather than hiding them behind an earlier read failure. An in-memory
segment has no persisted path and needs no file-cache load.

The loader never writes segment content, changes mappings, advances the writer
generation, or selects retained inactive snapshots. The existing lexical warm-up
runs afterward to initialize query metadata and verify the query path.

Treat the loaded pages as reclaimable operating-system cache, not pinned memory
or an application-owned copy. A deployment that depends on cold-search latency
must leave enough memory for the active persisted roots, the node heap, the
document vault working set, and operating-system headroom. It must validate
synchronized queries on its actual corpus. Hundreds of simultaneous broad
queries require enough independently measured read replicas, or a separately
accepted bounded-retrieval backend, to keep each replica below its measured
CPU and cache limit. This decision does not add that runtime service boundary.

## Consequences

The node does predictable sequential search-index I/O before listeners open
instead of allowing unrelated first users to trigger scattered faults while
holding all interactive admissions. Startup duration increases with active root
bytes and storage throughput even when no consolidation is needed. The active
file-backed pages contribute to process and cgroup residency and may be reclaimed
later; a target whose active index does not fit its available cache cannot claim
the same cold latency.

Tests prove sequential shard and path order, real active-root selection, exact
file content, size, mode, and modification-time preservation, empty-file
acceptance, cancellation before and during work, missing and nonpersisted path
handling, invalid snapshot results, no-progress, negative, and oversized reader
refusal,
and joined read, file-close, and snapshot-close failures. No stored format,
dependency, setting, environment variable, listener, service, image, package
topology, contract, or wire shape changes.
