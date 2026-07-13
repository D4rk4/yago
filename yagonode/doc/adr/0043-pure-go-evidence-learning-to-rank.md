# 0043. Pure-Go evidence learning-to-rank

Date: 2026-07-11

## Status

Accepted

## Context

ADR-0035 established an offline coordinate-ascent ranker over a small ranking
profile. The production search pipeline now has enough persistent evidence and
judgments to support document-level learning-to-rank, but the node must remain
usable on CPU-only Linux systems. It cannot require a GPU, external API, model
sidecar, separate trainer, native plugin, or dynamic model runtime.

Serving and evaluation also had correctness gaps: retrieval caches omitted
request fields, remote fusion depended on completion order, publication time
could be confused with fetch time, missing quality evidence was positive, click
evidence was not tied to a signed impression, and ranking policy ran in more than
one stage.

The first learned-model formats also collapsed an observed zero and missing
evidence into one value. Training did not include the serving RM3 stage, global
fusion could consume the local learned window with foreign rows, local-only
evaluation reported peer resources as zero, and click aggregates did not provide
an operationally separated online-comparison and implicit-qrel path.

## Decision

YagoRank uses a bounded in-process pipeline:

1. Strict all-term fielded BM25 forms every initial local candidate set. Queries
   with at least three distinct terms also run a relaxed branch requiring the
   ceiling of 60% term coverage; strict matches precede relaxed-only matches after
   fusion and before stored documents are inspected. Bounded RM3 widens recall
   without reducing either branch's coverage rule. Quoted phrases add a
   bounded positive preference only when analyzer-normalized terms are adjacent
   in one stored field of a leading evidence candidate. Other phrase and proximity
   evidence is then derived from visible text and, for explicit ranking-feature,
   explain, or near consumers, capped stored-document positions. Ordinary
   retrieval issues no Bleve phrase or location query. Local and YaCy peer lists
   use deterministic RRF. Global serving retrieves at least twice the learned
   window from the merged list and scans only until its bounded local candidate
   window is full, preserving peer and web slots.
2. Crawl and peer evidence is persisted before search: lifecycle dates, inbound
   anchors, content quality and safety, exact and near-duplicate clusters,
   registrable-domain authority, and decayed peer reputation.
   Domain authority rejects invalid and same-domain links, caps every domain edge
   at eight distinct source-page votes, and reads a deterministic 16 MiB sample
   of at most 3,276 cross-domain votes. An operator can persist at most 256
   canonical domain names or IP literals and a TrustRank blend in `[0,1]` in the vault; the
   default list is empty. Authenticated GET and PUT at
   `/api/admin/v1/search/ranking/trust` and the YagoRank console edit the same
   policy, and a change triggers an immediate authority refresh.
3. Candidates carry a fixed 33-signal evidence vector plus a presence mask.
   Current `v2` linear and histogram formats exclude unknown values from robust
   centers and scales. Linear unknowns contribute zero. Histogram thresholds use
   observed values only, and a tree path that reaches a missing split terminates
   with zero contribution. Readers preserve `v1` zero-imputation and tree-routing
   behavior and deterministic legacy reserialization, while newly trained models
   use `v2`.
4. A signed linear LambdaRank model is the low-data learned model. Production
   histogram LambdaMART defaults to 64 trees, depth 4, and 32 bins. Every tree
   selects one named interaction allowlist: candidate retrieval, field and term
   dependence, content quality, temporal authority, federation support, or a
   small cross-family relevance-quality set. Multiple features within that set
   may occur on one path.
5. Active snapshots are versioned JSON in the vault. Status and snapshot are one
   atomic view. Compare-and-swap activation rejects a proposal if its evaluated
   incumbent changed and keeps eight rollback revisions.
6. Query-clustered train, development, and test partitions, including dated
   chronological clusters, gate promotion. One frozen candidate pool compares
   lexical baseline, active incumbent, and proposal. The proposal must beat both
   references by at least 2% held-out NDCG with a non-negative 95% cluster-level
   paired-bootstrap lower bound over at least 20 independent query clusters. It
   rejects recall, discounted top-10 safety/spam exposure, slice, and p95 rerank
   wall-latency regressions. Peer bytes and timeouts are nullable and gate a
   comparison only when both arms measured them.
7. HMAC-bound impressions run Team Draft between a comparable active revision
   and the lexical baseline after preparation returns successfully. The request path waits
   at most 50 milliseconds and retains at most four context-insensitive tasks.
   Capacity, a planning timeout, or a persistence error returned within the
   budget preserves original ordering without capture metadata; persistence
   pending at the deadline continues independently in its retained slot until it
   returns. A click waits for the matching persistence outcome, and a handed-off
   token whose persistence fails stays rejected through expiry in a bounded
   registry that stops new preparation at capacity. Shutdown joins every admitted
   task before storage closes. Aggregate Team Draft credit is
   only online comparison evidence. Without a comparable revision, adjacent
   FairPairs use equal randomized exposure. Only a FairPairs winner whose 95%
   Wilson interval excludes an even click split after the minimum evidence
   threshold becomes an implicit qrel. Legacy pointwise aggregates are not
   qrels, and curated judgments replace implicit evidence for the same query.
8. Learned scoring applies only to locally stored candidates until representative
   federated training evidence exists. Safety, persistent cluster consolidation,
   MMR, host crowding, date order, and paging run once afterward.

Coordinate ascent remains available for profile-weight preview and cold-start
diagnosis. A node without an active learned snapshot follows the complete
lexical path.

## Bounds

The learned candidate window is 100 and cannot exceed 256. Training accepts at
most 1,000 queries and retrieves at most 200 candidates per query. The complete
pool is limited to 200,000 results, 100,000 model examples, 8,000,000 feature
values, and 1,000,000 preference pairs. The histogram model is limited to 64
trees, depth 4, and 32 bins. RM3 uses five documents, 256 tokens per document,
and three expansion terms. Model history, peer evidence, click aggregates, HMAC
replay state, safety training data, content-cluster candidate sets, trusted
domains, and authority citations have fixed limits.

## Consequences

Search remains pure Go and CPU-only. Training is slower than coordinate ascent
but is admin-triggered, context-cancellable, and bounded. Learned snapshots can
only change serving after held-out promotion or explicit rollback. YaCy wire
formats and RWI exchange remain unchanged.

New model snapshots distinguish missing evidence without changing the result of
loading an existing `v1` snapshot. Evaluation does not invent zero-valued peer
resource measurements, and click-derived qrels now require randomized pairwise
evidence with a statistically decisive outcome.

The model is limited to stored lexical, graph, quality, safety, temporal, and
peer signals. It does not provide dense semantic retrieval. That is an explicit
tradeoff for deployment independence and predictable resource use.

## Research basis

- [From RankNet to LambdaRank to LambdaMART](https://www.microsoft.com/en-us/research/publication/from-ranknet-to-lambdarank-to-lambdamart-an-overview/)
- [Query-level stability and generalization in learning to rank](https://www.microsoft.com/en-us/research/publication/query-level-stability-and-generalization-in-learning-to-rank/)
- [ILMART: Interpretable LambdaMART](https://arxiv.org/abs/2206.00473)
- [Position-bias estimation for unbiased learning to rank](https://research.google/pubs/position-bias-estimation-for-unbiased-learning-to-rank-in-personal-search/)
- [Addressing trust bias for unbiased learning to rank](https://research.google/pubs/addressing-trust-bias-for-unbiased-learning-to-rank/)
- [Efficient online evaluation with interleaving](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/fp041-schuthA.pdf)
