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

## Decision

YagoRank uses a bounded in-process pipeline:

1. Strict and relaxed fielded BM25, phrases, SDM, and bounded RM3 form the local
   candidate union. Local and YaCy peer lists use deterministic RRF.
2. Crawl and peer evidence is persisted before search: lifecycle dates, inbound
   anchors, content quality and safety, exact and near-duplicate clusters,
   registrable-domain authority, and decayed peer reputation.
3. Candidates carry a fixed 33-signal evidence vector. A signed linear
   LambdaRank model is the low-data learned model. A bounded histogram
   LambdaMART model is available for larger judgment sets.
4. Active snapshots are versioned JSON in the vault. Status and snapshot are one
   atomic view. Compare-and-swap activation rejects a proposal if its evaluated
   incumbent changed and keeps eight rollback revisions.
5. Query-clustered train, development, and test partitions, including dated
   chronological clusters, gate promotion. One frozen candidate pool compares
   lexical baseline, active incumbent, and proposal. The proposal must beat both
   references by at least 2% held-out NDCG with a non-negative 95% cluster-level
   paired-bootstrap lower bound over at least 20 independent query clusters. It
   rejects recall, discounted top-10 safety/spam exposure, slice, latency,
   peer-byte, and peer-timeout regressions.
6. HMAC-bound randomized impressions provide aggregate clipped IPS and SNIPS
   click evidence. Curated judgments replace click evidence for the same query.
7. Learned scoring applies only to locally stored candidates until representative
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
replay state, safety training data, and content-cluster candidate sets have fixed
limits.

## Consequences

Search remains pure Go and CPU-only. Training is slower than coordinate ascent
but is admin-triggered, context-cancellable, and bounded. Learned snapshots can
only change serving after held-out promotion or explicit rollback. YaCy wire
formats and RWI exchange remain unchanged.

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
