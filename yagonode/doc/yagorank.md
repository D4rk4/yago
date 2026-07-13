# YagoRank

YagoRank is the ranking pipeline used by local and federated search. It runs in
the `yago-node` process, uses Go code and persisted model data only, and does not
require a GPU, an external API, a sidecar, a native model runtime, or a separate
training binary. YaCy RWI remains the peer exchange format and is not changed by
ranking.

## Serving pipeline

Serving, explanation, and training use the same bounded local candidate stages:
local retrieval, bounded RM3 feedback, then lexical evidence construction. Every
search follows these stages:

1. Build a candidate window from strict all-term fielded BM25 and a relaxed 60%
   term-match branch, then fuse the two lists by reciprocal rank before stored
   documents are inspected. Bounded RM3 feedback uses at most five distinct
   documents, 256 tokens per document, and three expansion terms that occur in
   at least two feedback documents. Required query terms remain mandatory.
   Visible text supplies ordinary lexical proximity; explicit ranking-feature,
   explain, or near consumers may request capped stored-document positions.
2. Merge local and YaCy peer lists deterministically. A peer contributes one
   list regardless of response timing. Persistent peer reliability adjusts its
   RRF contribution, while IPv4 `/24` and IPv6 `/48` influence caps limit a
   network group's aggregate effect. Search reads an immutable in-memory
   reputation snapshot; a bounded worker persists observations and refreshes
   that snapshot after writes and every five minutes outside the request path.
   It retries one monotonic batch with capped backoff until persistence or
   shutdown, reconciles a superseded sequence, stops admission before draining,
   and gives concurrent shutdown callers one bounded completion point. A failed
   refresh keeps the last good snapshot. Unresolved hostname peers share one
   conservative influence group.
3. Attach an immutable 33-signal evidence vector to each candidate. A presence
   mask distinguishes an observed zero from missing evidence. Missing values do
   not affect robust normalization or a linear score, and an explanation marks
   them as unknown and unused.
4. Apply the active learned model to at most 100 locally stored candidates.
   Federated and web-fallback rows keep their fusion order because the training
   set contains no representative federated evidence. A node without an active
   model keeps the complete lexical ranking path. Global search requests at
   least twice the learned window from the merged list and scans it only until
   the bounded local window is collected; peer and web slots are not consumed
   as local model capacity.
5. Apply safety policy, persistent content-cluster consolidation, MMR, host
   crowding, requested date ordering, and paging once. Similar unclustered
   results are not deleted.

Index-cache identity covers every request field and ranking weight. Its
byte-aware LRU retains at most 256 entries and 16 MiB, deep-clones owned payloads,
and clears stale generations immediately after an index mutation. Paging
sessions structurally include every result- or policy-affecting request field
except offset and limit; their byte-aware LRU retains at most 128 sessions and
32 MiB. Both caches serve an oversized result once without retaining it. Cached
strings, facets, maps, positions, and media values are deeply detached before
retention and delivery. Disk post-filters and facets traverse matching documents
with a bounded identifier cursor and retain only a bounded score top-k, so an
eligible tail and counts beyond 1,000 matches remain visible without unbounded
memory. Candidate-only disk scans read a stored size-bounded projection instead
of full document bodies; selected evidence may still load the leading ten full
documents. A complete scan has a five-second internal deadline, 100,000-hit cap,
256-hit page cap, and bounded identifier cursor. Bleve locations remain
disabled; explicit position maps come from the capped stored-document evidence
pass. Deadlines and hit caps bound explanations and stored evidence.
Deadline, cap, incomplete page, and partial-shard conditions fail honestly
instead of returning truncated counts. The scan keeps one consistent index view,
so indexing can wait behind work bounded by both controls.

## Candidate evidence

The learned feature catalog is fixed and versioned. It includes:

- strict, relaxed, feedback, local, remote, and fused retrieval scores and ranks;
- title, heading, trusted inbound-anchor, URL, and body evidence;
- term coverage plus ordered, unordered, and whole-document proximity;
- signed content quality, spam risk, text-shape measures, and missingness;
- publication-date confidence and multi-timescale freshness;
- registrable-domain authority and authority confidence;
- URL prior, source count, peer support, and peer reputation.

Publication and modification dates come from structured page metadata, HTTP
headers, or sitemap evidence with source confidence. First-seen and actual
content-change timestamps are stored separately. Fetch time is never presented
as publication time, future dates are rejected, and an unchanged recrawl cannot
refresh a page's rank.

Inbound anchors are replaced atomically when a source page is recrawled. Links
marked `nofollow`, `ugc`, or `sponsored` do not contribute ranking anchor text or
authority. Domain authority uses a bounded in-process link graph with PageRank
and TrustRank-style teleport evidence, distinct-source limits, and confidence.
Graph refresh retains a deterministic hash-priority sample of at most 1,048,576
unique citations, so scanning a large document collection cannot create an
unbounded citation slice.

TrustRank teleport seeds are an operator policy stored in the node vault. The
default policy is empty; operators can select a blend in `[0,1]` and at most 256
canonical domain names or IP literals. A policy change refreshes domain
authority immediately rather than waiting for the periodic refresh.
Admin deletion, quota eviction, redirect purge, and crawl tombstones share one
lineage cleanup that removes the source's anchors and cluster membership, then
refreshes any surviving representative in storage and search.

Content quality is computed during ingest as signed evidence in `[-1,1]`.
Unknown, short, or unsupported-script evidence is neutral. It never receives a
maximum-quality bonus.

Exact content identity is assigned before near-duplicate clustering. Near
duplicates use bounded shingles, SimHash LSH candidates, and Jaccard
confirmation. Every URL remains stored; the final list selects a deterministic
representative by canonical declaration, quality, authority, and URL order.
The committed canonical document, including merged lifecycle dates and inbound
anchors, is the exact value sent to clustering and the search index.

## Learned models

Two pure-Go model families are available:

- signed linear LambdaRank with robust per-query normalization, NDCG-weighted
  pair gradients, regularization, top-k constraints, and feature-sign bounds;
- histogram LambdaMART with at most 64 trees, depth 4, 32 bins, Newton leaf
  values, monotonic constraints, and feature-interaction allowlists.

The linear model is the preferred low-data model. Histogram LambdaMART is useful
only when the judgment set is large and representative enough to pass held-out
promotion. Coordinate ascent over the visible field/prior weights remains an
operator preview and a small-data fallback.

Newly trained snapshots use `yago-linear-lambdarank-v2` or
`yago-histogram-lambdamart-v2`. Robust centers, scales, and histogram thresholds
use observed values only. A missing histogram split terminates that tree with a
zero contribution instead of choosing a branch. Readers retain the `v1`
zero-imputation and tree-routing semantics, and a loaded legacy snapshot
reserializes in its original format.

Production histogram training defaults to 64 trees, depth 4, and 32 bins. Each
tree is restricted to one named allowlist: candidate retrieval, field and term
dependence, content quality, temporal authority, federation support, or a small
cross-family relevance-quality set. A tree can use multiple features from its
selected family, preserving bounded, inspectable interactions.

The vault stores the active revision and eight rollback revisions. Status and
snapshot are read as one atomic catalog view. Promotion uses compare-and-swap
activation, so a model cannot replace an incumbent that changed during training.
Rollback is atomic, and the active model is restored on restart.

## Evaluation and promotion

Training accepts at most 1,000 queries, retrieves at most 200 candidates per
query from the same lexical evidence boundary used by serving, and bounds the
full pool at 200,000 results, 100,000 model examples, and 1,000,000 preference
pairs. Candidate enumeration and pairwise gradients observe request
cancellation. Query clusters are kept together across deterministic train,
development, and test partitions; chronological holdout is supported when dates
are present.

Lexical baseline, active incumbent, and proposal are evaluated on one frozen
candidate pool. A proposal must beat both lexical ranking and the incumbent. An
equal learned score preserves the incoming lexical order.

The evaluation report includes Recall@100/200, NDCG@10, ERR@10, navigational
MRR, alpha-NDCG, intent coverage, duplicate-cluster rate, domain coverage,
unsafe and spam counts, discounted unsafe and spam exposure at top 10, and
p50/p95 rerank wall latency. Peer bytes and timeouts are nullable: local-only
evaluation leaves them unmeasured instead of reporting false zeros.

A proposal is promoted only when all gates pass:

- held-out relative NDCG gain is at least 2%;
- at least 20 independent held-out query clusters are present;
- the query-cluster paired-bootstrap interval at 95% confidence does not cross
  zero;
- Recall@100/200, discounted top-10 safety/spam exposure, and named query slices
  do not regress;
- p95 rerank wall latency does not regress beyond its configured evaluation
  policy;
- peer traffic and timeout gates pass when both compared arms measured those
  resources, and otherwise remain explicitly unavailable.

A rejected proposal is returned with its metrics and reasons but is not
activated.

## Click evidence

Click capture is off by default. When enabled, the result page creates a
short-lived HMAC-SHA256 impression token containing the normalized query, model
assignment, ordered result identities, positions, propensities, expiry, and a
random nonce. The click endpoint accepts only a URL and position present in that
signed impression. The request path waits at most 50 milliseconds for preparation
and persistence, with at most four context-insensitive tasks retained. Capacity,
a planning timeout, or a persistence error returned within the budget preserves
the original order without capture metadata. Persistence pending at the deadline
continues independently in its retained task slot until it returns. Node shutdown
joins every admitted impression task before closing storage. Replays and clicks
per impression are bounded. A click waits for the matching in-flight impression
commit before validation. An emitted token whose commit fails remains rejected
until its expiry, and the bounded failure registry stops issuing new impressions
at capacity instead of attributing the click to older aggregate evidence.

When an active learned revision has a comparable lexical order, a successfully
prepared impression team-draft interleaves that revision with the lexical
baseline. A successful persistence commit stores aggregate impression and click
credit for the two revisions. Team-draft evidence is an online comparison only and is never
converted into relevance labels. Without a comparable active revision, adjacent
FairPairs randomization measures exposure at propensity `0.5`.

Implicit judgments come only from FairPairs winners whose 95% Wilson interval
excludes an even click split after the configured minimum impressions and
clicks. Legacy pointwise click aggregates are readable for migration but do not
become qrels. Curated qrels replace implicit evidence for the same query.

## Safety model

Safe search first uses blocking structured labels from page metadata. Adult and
RTA ratings or `isFamilyFriendly=false` classify a page as explicit. A positive
publisher `isFamilyFriendly=true` claim is not trusted as general evidence and
does not bypass the optional pure-Go signed Unicode character n-gram logistic
classifier. Training accepts at most 256 labeled documents and 8,192 runes per
document. The model is versioned, persisted, and rollback-capable. Training
labels are not retained.

Explicit results are removed when safe search is requested. Unknown peer and web
results are also removed; unknown local text results remain eligible, while
unknown image results are removed. Tavily image fields require classified
general evidence when `safe_search` and `include_images` are both enabled.

## Operator surfaces

The YagoRank console shows the active revision and model kind, rollback
availability, held-out gain and confidence, split sizes, and promotion reasons.
It can train the linear or histogram model, roll back one revision, and edit the
vault-backed trusted-domain list and TrustRank blend.

The admin JSON endpoints are:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/admin/v1/search/ranking` | GET, POST | Read or replace lexical profile weights |
| `/api/admin/v1/search/ranking/tune` | POST | Preview coordinate-ascent weights |
| `/api/admin/v1/search/ranking/model` | GET | Read active model status and snapshot |
| `/api/admin/v1/search/ranking/model/train` | POST | Train, evaluate, and conditionally promote a model |
| `/api/admin/v1/search/ranking/model/rollback` | POST | Roll back the active ranking model |
| `/api/admin/v1/search/ranking/trust` | GET, PUT | Read or replace trusted domains and TrustRank blend |
| `/api/admin/v1/search/judgments` | GET, POST, DELETE | Manage curated qrels |
| `/api/admin/v1/search/explain` | POST | Inspect retrieval, learned contributions, tree paths, and final ranks |
| `/api/admin/v1/search/safety/model` | GET | Read content-safety model status |
| `/api/admin/v1/search/safety/model/train` | POST | Train and activate a bounded safety model |
| `/api/admin/v1/search/safety/model/rollback` | POST | Roll back the safety model |

## Runtime limits

YagoRank has no runtime network dependency. Training and inference are in-process
and CPU-only. Candidate windows, documents, tokens, features, histogram bins,
tree count, depth, snapshots, click tokens, replay state, peers, and network-group
influence are bounded. Search remains functional with no learned or safety model.

## Code map

| Concern | Location |
| --- | --- |
| Candidate retrieval and cache | `internal/searchindex`, `internal/searchlocal` |
| RM3, RRF, evidence, cluster policy, MMR | `internal/searchcore` |
| Learned inference and snapshots | `internal/learnedrank`, `internal/rankingmodel` |
| Linear LambdaRank and histogram LambdaMART | `internal/rankfit` |
| Held-out dataset and promotion | `internal/rankingtrain`, `internal/searcheval` |
| Click integrity and debiasing | `internal/clickcapture` |
| Dates, anchors, clusters, safety evidence | `internal/documentstore`, `internal/crawlresults` |
| Domain authority and peer reputation | `internal/hostrank`, `internal/peerreputation` |
| Admin runtime adapters | `internal/yagonode`, `internal/adminui` |

## Research basis

- [From RankNet to LambdaRank to LambdaMART](https://www.microsoft.com/en-us/research/publication/from-ranknet-to-lambdarank-to-lambdamart-an-overview/)
- [Query-level stability and generalization in learning to rank](https://www.microsoft.com/en-us/research/publication/query-level-stability-and-generalization-in-learning-to-rank/)
- [ILMART: Interpretable LambdaMART](https://arxiv.org/abs/2206.00473)
- [A Markov Random Field Model for Term Dependencies](https://doi.org/10.1145/1076034.1076115)
- [Reciprocal Rank Fusion](https://doi.org/10.1145/1571941.1572114)
- [NIST TREC relevance-model feedback](https://trec.nist.gov/pubs/trec17/papers/cmu.rf.rev.pdf)
- [Position-bias estimation for unbiased learning to rank](https://research.google/pubs/position-bias-estimation-for-unbiased-learning-to-rank-in-personal-search/)
- [Addressing trust bias for unbiased learning to rank](https://research.google/pubs/addressing-trust-bias-for-unbiased-learning-to-rank/)
- [Efficient online evaluation with interleaving](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/fp041-schuthA.pdf)
- [TrustRank](https://www.vldb.org/conf/2004/RS15P3.PDF)
- [Detecting near-duplicates for web crawling](https://doi.org/10.1145/1242572.1242592)
