# 24. Local host block-rank ranking signal

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

The substrate for a citation rank already existed. The node could collect an
incoming host-link graph from stored document outlinks and serve it at
`/yacy/idx.json?object=host`, but the original graph was refreshed from the
request path and nothing ranked with it.

Full YBR parity is large: it needs a persisted host graph, a peer consumer that
fetches other peers' tables, cross-peer aggregation, and a rank-table exchange
surface. Shipping that in one step risks a broad, half-tested change.

## Decision

Compute a **local** host block-rank from this node's own crawl graph and fold it
into result scoring. Distributed rank-table exchange is not part of the local
ranking contract.

- A new pure package `internal/hostrank` computes normalized authority evidence
  keyed by registrable domain via damped iterative rank propagation over the
  incoming citation graph. Unknown domains remain neutral rather than penalized.
- The shared completion-relative corpus loop (`runCorpusSignalRefreshLoop`)
  collects a bounded cross-domain citation sample alongside spelling and optional
  morphology frequencies. Invalid and same-domain links are discarded, and each
  domain edge retains at most eight distinct source-page votes before the global
  sample. The loop then republishes the table through an atomic `hostrank.Holder`,
  so the search path reads it lock-free and never scans the store inline.
- One bounded vault checkpoint atomically retains the last complete authority
  table, citation sample, spelling vocabulary, optional morphology vocabulary,
  YaCy host-link graph, trust policy, and completion time. Startup publishes the
  search signals and host-link snapshot before listeners open.
  A fresh checkpoint schedules the first scan for its original due time; a stale
  checkpoint stays available while an immediate replacement scan runs. Failed
  and cancelled passes leave the prior checkpoint intact.
- `RankingWeights` gains a `HostRank` coefficient, enabled by default at 0.3. It
  is a post-retrieval multiplier — a result's score is scaled by
  `1 + HostRank*rank(host)`
  and the local results are re-sorted — not a text-field boost, so it does not
  count toward the "at least one positive weight" relevance requirement and the
  Bleve field query ignores it. Setting the coefficient to zero disables the
  rescore.
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

Full distributed YBR — a peer idx.json consumer, cross-peer aggregation, and
rank-table exchange — is unnecessary for the shipped local authority signal and
is not a deferred ranking requirement. Accepting untrusted peer rank tables would
also add a Sybil surface and a separate compatibility protocol. Any future
exchange proposal therefore needs its own security and interoperability decision
rather than inheriting approval from this ADR.

## Consequences

Ranking gains a host-authority dimension that operators tune through the
positive `hostRank` weight in the existing `/api/admin/v1/search/ranking` profile
(the JSON round-trips the field). Authority shares one completion-relative
document pass with spelling, optional morphology signals, and the YaCy host-link
graph, so it adds no independent periodic scan. Stored documents are read through
fixed 16-document keyset pages through immutable last keys captured for the
legacy and admission-ordered partitions under a brief document-admission fence,
and each vault view is released before decoding and analysis. Later admissions
are included by the next pass. Peer host-link requests read the atomically published graph and never
trigger a document scan. The successful pass replaces one bounded checkpoint at
most once per refresh
interval; trust-policy changes may replace it without advancing its corpus
completion time. The
authority keys use the registrable domain derived from normalized source and
target URLs, so scheme, port, and subdomain differences do not split one domain's
evidence. Peer rank-table aggregation is outside the accepted ranking design.
