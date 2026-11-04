# 0085. Defer inbound-anchor target refresh and commit shards concurrently

Date: 2026-11-04

## Status

Accepted for implementation on 2026-11-04.

## Context

Each crawled page replaces the inbound anchors it contributes to its link
targets. Ingest rewrote every stored target document and re-indexed it in the
search index once per source page. A popular target linked from many pages was
rewritten and re-indexed once for each of them. Production measurement put this
target stage at more than half of ingest time, with about 96 storage flushes
and more than 5 MB written per ingested page.

One sharded-vault update committed its changed shards one after another. Each
shard commit waits for two flushes, so an update touching several shards paid
the flush latency of every shard in sequence.

Published search engines that maintain anchor text incrementally keep the
anchor store as the source of truth and refresh target index entries from a
background work list, so several anchor changes reach one target in one
re-index.

## Decision

Ingest stores the target's inbound-anchor contributions and, when a stored
target's effective inbound anchors change, writes a small durable refresh mark
naming the replaced sources. Ingest no longer rewrites or re-indexes the target
document. Marks for one target merge; a mark naming more than 256 sources covers
all sources.

A node background loop refreshes marked targets every ten seconds in pages of at
most 64 targets until no mark remains. Each page locks its targets, recomputes
their inbound anchors from the anchor store and offers changed documents to the
search index. It commits every changed document before a separate phase removes
the page's marks, retaining the same target locks and existing transaction row
and byte limits throughout both phases. An index or document-commit failure
keeps the marks for another pass. A cleanup failure may leave already-current
targets marked; replay removes those marks without rewriting or re-indexing the
documents. Missing targets also require only mark cleanup. Removing a mark is
still a storage write. Document revision reads apply a pending mark.

This ordering corrects the released v0.0.73 implementation, which stored a
document and removed its mark in one non-atomic cross-shard update. The
correction is unreleased as of 2026-11-04.

One sharded-vault update attempts every changed shard concurrently, waits for
all attempts and reports the lowest-numbered failing shard. Successful commits
are not rolled back when another shard fails. Shards commit independently;
neither one update nor the ordered refresh phases provide cross-shard atomicity.

Ingest's anchor rows and refresh marks also commit independently. A failed
target update leaves source publication unchanged but may retain partial anchor
or mark changes. The caller must replay the original replacement; reopening the
vault alone does not recover a missing mark or finish source publication.
Replaying the replacement converges even if a refresh ran between attempts;
a subsequent identical replacement leaves the completed state unchanged.

No setting or environment variable is added. No third-party dependency is
added.

## Consequences

Ingest writes one anchor row and at most one mark per changed target instead of
a full document and an index update. A target linked from many pages between two
refresh passes is rewritten and re-indexed once.

Search may show previous anchor text until a successful refresh. The ten-second
interval is not a completion bound: a backlog or failure can extend the delay.

Committed marks survive restart. Ordered cleanup retains them until document
commits succeed, but it cannot recreate a mark already lost by v0.0.73 or replace
replay of an interrupted ingest publication.

v0.0.72 ignores the refresh collection. Pending targets can remain stale after
rollback: an unchanged source replay skips target work, and a change from a
different source is not guaranteed to remove superseded anchors. Successful
drainage before rollback reduces this backlog but cannot repair marks already
lost by v0.0.73.

An update touching several shards waits for roughly the slowest shard flush
instead of the sum of all flushes. Storage devices with a single queue may see a
smaller gain.
