# Ranking research, July 2026

## Scope

The research target was a state-of-the-art ranking path compatible with the
existing Bleve document index and YaCy RWI federation. Deployment constraints
exclude GPUs, external APIs, sidecars, separate training binaries, native model
runtimes, and mandatory semantic services.

## Findings

Modern web ranking still benefits from a strong lexical candidate set before
learning-to-rank. The practical CPU-only path is fielded BM25 with term
dependencies, conservative relevance-model feedback, deterministic rank fusion,
document and graph evidence, and supervised listwise or pairwise reranking.

LambdaMART remains a strong tabular ranking method. ILMART shows that interaction
constraints can retain much of LambdaMART's quality while improving
interpretability. A signed linear LambdaRank model is a safer cold-start choice
when judgments are limited.

Clicks cannot be treated as direct relevance labels. Adjacent FairPairs provide
randomized equal exposure, and confidence intervals can reject inconclusive
pair outcomes. Team-draft interleaving compares two complete rankings online
without turning its click credit into relevance labels.

Federated result scores are not comparable across peers. Reciprocal-rank fusion
is stable without score calibration. Persistent reliability can weight a peer's
contribution, but network-group influence must be capped so multiple identities
from one address range cannot dominate.

Publication evidence must be separated from fetch time. Freshness works best as
a confidence-weighted, query-mode-dependent signal rather than a universal boost.
Anchor evidence, domain authority, content quality, safety, and duplicate
clusters must be computed from stored crawl data instead of request-time text
heuristics.

## Selected implementation

| Area | Selection |
| --- | --- |
| Candidate retrieval | Strict plus 60%-match fielded BM25, bounded RM3, then lexical evidence through one train/explain/serve sequence; global serving fetches a larger merged window only to collect its bounded local model window |
| Fusion | Deterministic RRF with decayed peer reliability and `/24`/`/48` group caps |
| Evidence | Fixed 33-signal immutable vector with a presence mask; unknown values are excluded from robust statistics and model contributions |
| Low-data model | Signed linear LambdaRank with neutral missing evidence and versioned legacy reads |
| Higher-data model | Histogram LambdaMART, 64 trees, depth 4, 32 bins, observed-only thresholds, neutral missing paths, and named interaction families |
| Authority | PageRank plus bounded citation sampling and a vault-backed operator TrustRank policy of at most 256 canonical domain names or IP literals |
| Evaluation | Query-cluster and chronological holdouts, cluster-level paired bootstrap, recall/NDCG/ERR/MRR/diversity/discounted safety exposure, rerank wall latency, and peer resources only when measured |
| Promotion | Candidate beats lexical and active incumbent on one frozen pool; at least 20 clusters, 2% held-out NDCG gain, and a non-negative 95% lower bound |
| Clicks | HMAC impressions; automatic Team Draft for active-versus-lexical comparison; otherwise adjacent FairPairs, with only 95%-confidence pair winners eligible as implicit qrels |
| Duplicates | Exact identity then bounded SimHash-LSH candidates with Jaccard confirmation |
| Final policy | Persistent cluster consolidation, MMR, host crowding, date ordering, paging, once |

Rejected options were neural cross-encoders, SPLADE, ONNX, FAISS, Qdrant,
LightGBM/Python training, and hosted reranking APIs. They violate the deployment
constraints or add a runtime boundary that the node does not need for this
feature set.

## Primary sources

- [From RankNet to LambdaRank to LambdaMART](https://www.microsoft.com/en-us/research/publication/from-ranknet-to-lambdarank-to-lambdamart-an-overview/)
- [ILMART: Interpretable LambdaMART](https://arxiv.org/abs/2206.00473)
- [A Markov Random Field Model for Term Dependencies](https://doi.org/10.1145/1076034.1076115)
- [Reciprocal Rank Fusion](https://doi.org/10.1145/1571941.1572114)
- [NIST TREC relevance-model feedback](https://trec.nist.gov/pubs/trec17/papers/cmu.rf.rev.pdf)
- [Position-bias estimation for unbiased learning to rank](https://research.google/pubs/position-bias-estimation-for-unbiased-learning-to-rank-in-personal-search/)
- [Addressing trust bias for unbiased learning to rank](https://research.google/pubs/addressing-trust-bias-for-unbiased-learning-to-rank/)
- [Efficient online evaluation with interleaving](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/fp041-schuthA.pdf)
- [TrustRank](https://www.vldb.org/conf/2004/RS15P3.PDF)
- [Detecting near-duplicates for web crawling](https://doi.org/10.1145/1242572.1242592)
