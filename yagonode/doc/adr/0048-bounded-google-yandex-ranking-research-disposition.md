# 0048. Adopt only bounded and independently justified ranking evidence

Date: 2026-07-15

## Status

Accepted

## Context

Open Google and Yandex material describes useful search-system shapes, but it
does not publish either company's current ranking formula. Google's current
guide names link analysis, freshness, passage ranking, original-content
selection, site diversity, neural matching, quality, and spam-control systems.
Yandex's current principles say that machine-learned formulas combine many query
and document properties. Patents describe possible implementations, not proof
that an exact method is deployed.

Yago already has fielded BM25, multilingual analyzers, strict and relaxed
candidate tiers, bounded RM3, stored lexical evidence, inbound anchors,
canonical and near-duplicate clusters, content quality, adaptive freshness,
background registrable-domain authority, deterministic fusion, MMR, randomized
online comparison, and bounded pure-Go learning to rank. New evidence must fit
those seams without a request-time corpus scan, external provider, GPU model, or
unbounded position index.

## Source boundaries

The following source categories have different roles:

| Source | What it establishes | What it does not establish |
| --- | --- | --- |
| Brin and Page's 1998 paper | Link analysis is established public information-retrieval research | Google's current PageRank implementation or weights |
| Metzler and Croft's 2005 paper | Ordered and unordered adjacent query-term dependence is established public research | Yago's fixed windows, confidence values, or vendor production behavior |
| Yandex 2003, ROMIP-2004, and ROMIP-2006 papers | Historical evidence for inflectional morphology, positional pairs, passage context, and original-query gaps | Current Yandex formulas, current weights, or suitability of ROMIP coefficients for the web |
| Google Search ranking-systems guide | Public behavior and system families, including treating subdomains as one site for general diversity | An executable formula or permission to reproduce a patented implementation |
| Yandex ranking principles and YATI engineering account | A staged learned architecture and offline document-side computation are plausible design patterns | Current production features, model parameters, or a latency result for Yago |
| YetiRank and YetiLoss papers | Reproducible offline learning objectives evaluated on stated collections | A serving improvement without representative Yago judgments and held-out evaluation |
| Patent publications | Claim language and design areas that the engineering review should avoid | Deployment evidence, patent validity, jurisdictional status, or freedom to operate |

No statement in this ADR claims that an active Google or Yandex algorithm has
been implemented in Yago.

## Engineering claim-boundary screen

Patent publications are used only to reject designs. The implemented paths stay
outside the following reviewed combinations:

| Publication | Reviewed combination | Yago boundary |
| --- | --- | --- |
| [US11416529B2](https://patents.google.com/patent/US11416529B2/en) | Candidate passages scored from query and passage term weights, collection specificity, TF, IDF, normalization, and minimum coordination | Relaxed admission is a binary exact-surface span check; Yago adds no passage TF, IDF, coordination score, or passage selection |
| [US12072896B2](https://patents.google.com/patent/US12072896B2/en) | Center-token proximity hit sets, distance-based query-token-density values, hit-set scores, and a document density score | Yago compares adjacent raw-query requirements directly; it has no center token, hit-set density, repeated-hit boost, or document density aggregate |
| [US8060501B1](https://patents.google.com/patent/US8060501B1/en) | Semantic list, list-item, title, and header structures used to redefine term distance | Yago forbids cross-field and cross-value proximity and uses ordinary token coordinates only |
| [US8041709B2](https://patents.google.com/patent/US8041709B2/en) | Same-domain results formed and presented as a collapsed result cluster | Yago keeps every result as an independent row and only stably defers the third and later site rows |
| [US7716225B1](https://patents.google.com/patent/US7716225B1/en) | A learned link weight based on link/document features or navigational behavior and used in propagated rank | Yago does not learn per-link feature weights or use click navigation to weight graph edges |
| [US8818999B2](https://patents.google.com/patent/US8818999B2/en) | Fuzzy proximity based on lead positions, probability distributions, and influence kernels | Fuzzy analyzer variants receive no proximity credit; exact token pairs use neither probability nor a kernel |
| [US7996379B1](https://patents.google.com/patent/US7996379B1/en) | Inter-document relationships derived from local term relationships and an initial document order | Yago computes query-pair evidence independently inside each document and builds no result-set term graph |
| [US9317591B2](https://patents.google.com/patent/US9317591B2/en) | Stored mappings from segmented query/target word pairs to weights and weighted target-string length | Yago has no semantic word-pair weight table or weighted target-string edit length |

This is an engineering prior-art and claim-boundary screen, not legal advice or
patent clearance. It does not evaluate every claim, continuation, jurisdiction,
status event, doctrine of equivalents, or later publication. Patent-database
status and assignee fields also carry their own accuracy disclaimers. A qualified
lawyer must perform any freedom-to-operate analysis required for a release.

## Evidence disposition

The categories below are final for the current architecture. No row is a
deferred ranking requirement.

| Candidate | Disposition | Request-time cost | Index or storage cost | Abuse, quality, and patent boundary |
| --- | --- | --- | --- | --- |
| Five field boosts, lexical blend, content quality, and short-URL prior | Already present | Existing bounded retrieval and final lexical score | Existing document projection only | Field length normalization, quality bounds, and held-out evaluation limit stuffing; no vendor formula is copied |
| Registrable-domain authority and trust | Already present | Lock-free bounded lookup | Shared corpus pass and atomic checkpoint | Distinct source limits, self-link rejection, and trust policy bound link farms and Sybil influence; no learned per-link weights |
| Query-dependent freshness | Already present | Existing date-intent and candidate-distribution work | Existing publication metadata | Date confidence and future-date rejection bound forged dates; query-log spikes are rejected as private and poisonable |
| Exact and morphology-aware ordered and unordered pairs | Implemented | Reuses the leading-ten evidence scan and existing positions | None | Exact pairs keep full confidence, analyzer pairs use lower confidence, one occurrence cannot satisfy two requirements, and fuzzy variants receive none; mixed alphanumeric identifiers remain mandatory exact witnesses in relaxed local passages and visible web evidence |
| Smooth original-gap agreement | Implemented | Linear two-pointer walk over capped positions in at most 50 rows; no I/O | None | Best-pair evidence prevents repeated-term accumulation; no center token, density aggregate, probability distribution, or influence kernel |
| Canonical representative and registrable-site diversity | Implemented | Bounded cluster selection and linear stable row deferral | Existing canonical and duplicate metadata plus pinned suffix data | Bad canonicals and shared hosting can mislead; quality, authority, stable order, and site/date bypasses limit harm; rows are not collapsed or deleted |
| Inbound anchors and RM3 feedback | Already present | Existing candidate evidence and bounded feedback | Existing anchor storage; no new postings family | Source diversity, trust, minimum feedback support, and drift caps limit anchor and expansion spam |
| Passage TF, IDF, and minimum coordination | Rejected | Additional per-passage scoring | New corpus statistics and evidence schema | Claim-adjacent reviewed combination and long-document stuffing risk; binary admission and pair evidence close the demonstrated gap |
| Structural link-position weights | Rejected | A lookup could be bounded | Crawler-contract, document, graph, and checkpoint migrations | Navigation templates can manufacture weights; learned structural weights are claim-adjacent and add no demonstrated gap over trusted anchors and authority |
| Corpus-selected phrases | Unnecessary | Lookup could be bounded | Multilingual mining, postings, migration, and another corpus pass | Exact quotes, adjacent pairs, original-gap agreement, and RM3 cover the measured failures without phrase-stuffing pressure |
| Generative doc2query expansion | Rejected | No request cost after indexing | Transformer inference, model storage, dependency review, and expanded postings | Anchors, morphology, and RM3 cover the current vocabulary gap; there is no representative measured gain to justify a new model and poisoning surface |
| Successful-query document text | Rejected | Lookup could be cheap | Sensitive query retention and bias controls | Privacy, popularity bias, and poisoning outweigh an unmeasured gain; inbound anchors remain the accepted external vocabulary |
| YetiLoss or another trainer objective | Unnecessary | Frozen serving cost is unchanged | Another trainer and representative judgments | Existing LambdaRank and LambdaMART already share a frozen held-out promotion gate; another objective is not a missing serving signal |
| Separate click position-bias estimator | Unnecessary | None | More aggregate evidence and sufficient randomized exposure | Team Draft and decisive FairPairs already randomize exposure and avoid raw-CTR trust loops |
| Semantic teacher or dense reranker | Rejected | A request-time transformer has no measured reserve in the two-second path | Embeddings, model storage, corpus scoring, and dependency review | Semantic poisoning, drift, opaque failure, and a new runtime boundary outweigh unproven benefit for the measured lexical failures |

## Operator-control boundary

YagoRank exposes all thirteen operator-safe live coefficients: five field
boosts, host authority, freshness, content quality, short-URL prior, ordered and
unordered stored proximity, final lexical blend, and original-gap agreement.
Every control is persisted, validated, read on each new search, included in the
relevant cache identity, exercised by the coordinate-ascent preview, and shown
with its effective value and range.

The remaining numeric policies are deliberately not controls. Candidate and
evidence windows bound latency; exact/analyzer confidence and relaxed quorum
define evidence correctness; quoted-phrase caps, RM3 mixing, and expansion
limits bound drift; RRF and remote calibration make source scores comparable;
MMR and site caps enforce diversity; graph damping and adaptive freshness
mixtures define normalized evidence; safety and abuse thresholds are
enforcement gates. Changing one of these is an algorithm or safety-policy
change that requires code, tests, and evaluation, not an unchecked runtime
weight. Learned-model feature weights remain model state and can only change
through held-out promotion or rollback.

## Decision

1. Use registrable-domain identity for general result crowding, retaining exact
   normalized host identity for IP literals, public suffixes, localhost, and
   malformed hosts. Site-restricted and explicit date-order searches remain
   non-diversified.
2. Keep morphology-aware pair evidence and exact original-gap agreement within
   the bounds in [ADR-0047](0047-morphology-and-positional-ranking-evidence.md).
   Do not add passage TF/IDF, center-token density, semantic-list distance,
   fuzzy influence kernels, or stored semantic word-pair weights.
3. Keep authority, canonical selection, freshness, anchors, randomized
   evaluation, and the learned promotion gate already present. Extend those
   systems only through their existing bounded component seams.
4. Do not add YetiLoss, a separate position-bias estimator, corpus phrase
   mining, structural link-position weights, or a semantic teacher. They either
   duplicate the accepted mechanisms, lack the evidence required to produce a
   trustworthy signal, or violate the current privacy, abuse, patent, or runtime
   boundary. Literature results on another collection are not a promotion
   result.
5. Do not copy leaked factors, patent formulas, query-specific word lists, or
   reported vendor weights. New signals must beat the lexical and learned
   baselines on held-out judgments and preserve the two-second end-to-end
   budget.

## Latency interpretation

Registrable-site diversity is bounded but not literally free. Repeated local
microbenchmarks show a several-microsecond median increase and roughly ten to
eleven additional allocations over exact-host deferral for 10- and 50-result
windows. That establishes only a small, non-zero CPU cost on one machine. It is
not an end-to-end p95 result, a concurrency test, or proof for production load.
The full search latency gate remains required.

Morphology-aware proximity performs no second document scan and gap agreement
uses positions already collected for ranking. Sequential microbenchmarks that
show unchanged allocations or no material regression are supporting evidence,
not permission to describe the features as zero-latency. End-to-end p95 and the
two-second service budget decide acceptance.

## Consequences

The shipped additions are query-neutral, pure Go, bounded by existing candidate
windows, and require no new dependency, index field, persistent corpus signal,
or external service. They improve sibling-subdomain diversity and distinguish
exact, compact inflectional, ordered, and scattered wording while retaining the
original query gaps.

Costly or claim-adjacent alternatives have a final disposition rather than an
open implementation promise. This reduces engineering and abuse risk, but it
does not replace legal review and does not guarantee that every patent claim or
jurisdiction has been considered.

## Primary sources

- [Google Search ranking systems guide](https://developers.google.com/search/docs/appearance/ranking-systems-guide)
- [The Anatomy of a Large-Scale Hypertextual Web Search Engine](https://research.google/pubs/the-anatomy-of-a-large-scale-hypertextual-web-search-engine/)
- [A Markov Random Field Model for Term Dependencies](https://ciir.cs.umass.edu/pubfiles/ir-387.pdf)
- [A fast morphological algorithm with unknown word guessing induced by a dictionary for a web search engine](https://download.yandex.ru/company/iseg-las-vegas.pdf)
- [Yandex at ROMIP-2004: some aspects of full-text search and ranking](https://download.yandex.ru/company/experience/romip2004/romip2004_aspects.pdf)
- [Yandex text-ranking algorithm at ROMIP-2006](https://download.yandex.ru/company/03_yandex.pdf)
- [Yandex search ranking principles](https://yandex.ru/company/rules/ranking)
- [Yandex's YATI engineering account](https://habr.com/ru/companies/yandex/articles/529658/)
- [Winning the Transfer Learning Track with YetiRank](https://proceedings.mlr.press/v14/gulin11a.html)
- [Which Tricks Are Important for Learning to Rank?](https://proceedings.mlr.press/v202/lyzhin23a.html)
- [Position Bias Estimation for Unbiased Learning to Rank](https://research.google/pubs/position-bias-estimation-for-unbiased-learning-to-rank-in-personal-search/)
