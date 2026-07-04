# 18. Commit to Bleve as the web-search backend

Date: 2026-07-03

## Status

Accepted

Amends [ADR-0012](0012-use-bleve-for-embedded-full-text-fallback.md).

## Context

ADR-0012 adopted Bleve v2 as an embedded full-text fallback and named a Tantivy
Rust sidecar as the eventual production backend. The product target is a
self-hostable node that holds on the order of one to two million indexed pages and
answers web searches as fast as possible.

At that scale the Tantivy migration is not justified. Bleve v2 already uses the
scorch segment backend, which memory-maps its segment files, so most of the index
lives in the operating-system page cache rather than the Go heap. An index for one
to two million pages fits in memory on a modest host, so queries are CPU-bound and
the remaining gap to a Rust engine is a constant factor. A Tantivy sidecar buys
that constant factor at the cost of a second runtime, a process boundary with
per-query serialization, and the loss of the single pure-Go, CGO-free appliance
the project targets.

## Decision

Commit to Bleve as the local web-search backend and drop the Tantivy production
sidecar from the roadmap. Invest in tuning Bleve for web search instead:

- a custom index mapping that indexes only the queried fields, without the `_all`
  composite field, stored fields, term vectors, or doc values the node does not
  use;
- a bounded hot-query result cache with generation-based invalidation, and index
  warmup on open;
- web-search relevance and analyzer tuning behind the `SearchIndex` seam.

RWI stays the YaCy peer-to-peer exchange and DHT-interop format, never the primary
local index. The `SearchIndex` interface remains the single seam, so a different
backend can still be adopted later if a measured need appears.

## Consequences

The roadmap no longer carries a Rust runtime boundary. Backup and restore still
include both `yago-node.db` and `search.bleve`. Search performance now depends on
keeping the working set in memory and on the Bleve tuning above rather than on a
future engine swap. If a deployment ever outgrows Bleve, the decision is revisited
against a measured latency target instead of being taken preemptively.
