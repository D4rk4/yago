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
their inbound anchors from the anchor store, offers changed documents to the
search index, then stores each changed document and removes its mark under the
existing transaction row and byte limits. A failure keeps the mark for the next
pass. Document revision reads apply a pending mark, so lineage and replacement
logic always see current anchors.

One sharded-vault update commits its changed shards concurrently and reports the
failure of the lowest-numbered failing shard. Cross-shard atomicity is
unchanged: shards were already committed independently.

No setting or environment variable is added. No third-party dependency is
added.

## Consequences

Ingest writes one anchor row and at most one mark per changed target instead of
a full document and an index update. A target linked from many pages between two
refresh passes is rewritten and re-indexed once.

Search may show a target's previous anchor text for up to one refresh pass after
a source page changes. Anchor text is a ranking signal, so this delay does not
remove documents from results.

Marks are durable, so a restart or index failure delays a refresh but does not
lose it.

An update touching several shards waits for roughly the slowest shard flush
instead of the sum of all flushes. Storage devices with a single queue may see a
smaller gain.
