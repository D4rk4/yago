# 24. Local host block-rank as an opt-in ranking signal

Date: 2026-07-05

## Status

Accepted

## Context

YaCy ships Block Rank (YBR): a per-host authority score derived from the web
citation graph — hosts linked to by many well-linked hosts rank higher — that is
folded into result ranking and, in a full swarm, exchanged and aggregated across
peers. This node had none of it: search results were ordered by field-weighted
relevance only (`RankingWeights` over title/headings/anchors/body/url), with
host-based signals limited to crowding *diversity*, never authority.

The substrate for a citation rank already exists. The node builds an incoming
host-link graph from stored document outlinks (`storedDocumentHostLinks`,
`collectDocumentHostLinks`) and serves it at `/yacy/idx.json?object=host`, but the
graph is recomputed per request and thrown away, and nothing ranks with it.

Full YBR parity is large: it needs a persisted host graph, a peer consumer that
fetches other peers' tables, cross-peer aggregation, and a rank-table exchange
surface. Shipping that in one step risks a broad, half-tested change.

## Decision

Compute a **local** host block-rank from this node's own crawl graph and fold it
into result scoring as an **opt-in** signal, deferring the distributed exchange.

- A new pure package `internal/hostrank` computes a normalized authority `Table`
  (`map[hostHash]float64`, top host = 1) via damped iterative rank propagation at
  host granularity over the incoming citation graph. Unknown hosts rank 0, so they
  are neutral rather than penalized.
- A background loop (`runHostRankRefreshLoop`, mirroring the recrawl/eviction
  sweeps) rescans the document store on a coarse interval and republishes the
  table through an atomic `hostrank.Holder`, so the search path reads it lock-free
  and never scans the store inline.
- `RankingWeights` gains a `HostRank` coefficient (default 0 = off). It is a
  post-retrieval multiplier — a result's score is scaled by `1 + HostRank*rank(host)`
  and the local results are re-sorted — not a text-field boost, so it does not
  count toward the "at least one positive weight" relevance requirement and the
  Bleve field query ignores it. With the default weight the rescore is a no-op.
- The signal applies to local full-text results in `searchlocal`; remote results
  keep their calibrated federated scores.

## Considered alternatives

Query-time host boosting inside the Bleve query was rejected: it would require
indexing a host field and a per-host boost lookup, a more invasive change than a
post-retrieval multiply, and it would not reach remote results any more than the
chosen seam does.

Lazy per-query recomputation of the table was rejected: a full document-store scan
on the search path, even memoized, spikes query latency on large indexes. A
background refresh keeps the query path cheap.

Computing authority as raw inbound host in-degree (1-hop) was rejected in favor of
iterative propagation, which is the actual BlockRank mechanic (authority flows
through the graph) and is only marginally more code.

Full distributed YBR now — persisted graph, peer idx.json consumer, cross-peer
aggregation, rank-table exchange — was deferred to keep this slice self-contained
and fully tested. Local authority from our own crawl graph delivers the core
ranking value; peer aggregation extends coverage to hosts we have not crawled.

## Consequences

Ranking gains a host-authority dimension that operators enable by setting a
positive `hostRank` weight via the existing `/api/admin/v1/search/ranking` profile
(the JSON already round-trips the new field). Left at the default it changes
nothing. The node runs one more coarse-interval background scan of the document
store. The host hash is derived as `HashURL(url).HostHash()` to match the graph
builder; results whose URL scheme/port differ from how their host appears in the
crawl graph may miss the table and fall back to a neutral rank — acceptable for a
soft signal. Distributed host-rank exchange and aggregation across peers remain
future work (the second slice of the YBR epic).
