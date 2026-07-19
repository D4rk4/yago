# 8. Evict RWI postings under quota pressure

Date: 2026-06-20

## Status

Accepted

## Context

The node stores accepted RWI postings and their URL metadata until the storage quota is reached, then
returns backpressure and refuses further postings (ADR 0005). First-arrival data is retained forever and
fresh data is rejected once the store fills.

A DHT RWI node is responsible for serving a word-hash range, and its value to the network is current,
in-demand coverage of that range. Retaining the oldest bytes and refusing new postings freezes the node
at a stale snapshot, under-serves the word-hash range network-wide, and lets dead or changed URLs mislead
search results. The project also prefers availability and data integrity over ingestion throughput.

Outbound DHT distribution of stored postings is a non-goal today, but a future node may redistribute
postings it is about to evict to the peers responsible for their word-hash ranges, as upstream YaCy does.

bbolt never returns freed pages to the operating system; deleting records reclaims pages for reuse and
stops file growth rather than shrinking the file. Quota pressure must therefore be measured against
bbolt's used bytes, not the on-disk file size.

## Decision

Treat the store as a bounded, demand-weighted cache of its word-hash range rather than an archive. When
used bytes cross a high-water mark, a background single-flight sweeper evicts the least valuable URLs and
their postings down to a low-water mark, so the node keeps accepting fresh postings. The first eviction
signal is posting freshness from the existing RWI entry; least-recently-served eviction is a later
refinement.

Eviction is exposed as two storage primitives behind a narrow interface separate from `RWIStore`:

- candidate selection that returns the least valuable URL hashes, oldest first, until a target size is
  reached;
- atomic deletion of a URL together with all of its postings, its referenced-URL entry, and the matching
  count adjustments, at URL granularity so no postings or metadata are orphaned.

A pluggable eviction policy consumes selected candidates. The first policy drops candidates immediately.
A later redistribute policy groups a URL's postings by word hash, hands each group to the peer responsible
for that range, and deletes the URL only once every group is acknowledged, leaving selection and storage
untouched.

Refusing postings remains only as the safety valve for genuine disk exhaustion, when eviction cannot free
space fast enough to durably retain a new record.

If the normal metadata-backed candidate path is exhausted while usage remains above its target, the
sweeper uses a separate posting-only fallback. One pass inspects at most 4,096 stored postings from a
retained cursor and selects only URL hashes that have no URL-metadata row. Purging still removes the
complete posting lineage for that URL; metadata-backed URLs remain on the freshness path.

## Considered alternatives

Store-forever then refuse, the prior behavior, was rejected because it freezes the node at its first fill
and under-serves its word-hash range as content ages.

Age-only time-to-live eviction independent of quota was considered as the sole policy. It is kept as
optional hygiene but rejected as the primary mechanism because it does not bound storage and discards
fresh, in-demand postings whenever they are simply old.

A single atomic select-and-delete method was rejected because redistribution needs an outbound,
acknowledged transfer between selecting a candidate and deleting it, which cannot run inside a write
transaction; splitting selection from deletion lets the redistribute policy reuse both primitives without
reshaping storage.

Adding the eviction methods to `RWIStore` was rejected to keep that interface narrow; eviction is a
distinct capability with its own policy seam.

## Known limitations

Posting-only recovery is bounded rather than exhaustive in one pass and can require several sweeps to
cross the target. A pass that finds posting-only candidates but deletes no posting stops instead of
spinning. This fallback does not change the freshness order for metadata-backed URLs. Eviction-time
redistribution remains future work.

## Consequences

The node keeps serving fresh, in-demand postings for its word-hash range within a bounded store, and
operators observe eviction through metrics for eviction count, bytes reclaimed, and occupancy. The bbolt
adapter gains an eviction implementation that measures used bytes through database statistics. The
select/delete primitives and policy sink make eviction-time DHT redistribution an additive change rather
than a rewrite.
