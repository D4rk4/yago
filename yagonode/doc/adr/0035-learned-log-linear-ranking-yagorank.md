# 0035. Learned log-linear ranking (YagoRank)

Date: 2026-07-08

## Status

Superseded by [ADR-0043](0043-pure-go-evidence-learning-to-rank.md)

## Context

The goal is a results-ranking stack decisively stronger than Java YaCy's, built
under our standing constraints: pure-Go, CPU-only, no GPU or Python/ONNX runtime,
no new third-party dependency without its own ADR, and KISS — explicitly *not*
becoming "a slow monster like YaCy."

A grounded re-audit of our own code (SEARCH R&D, 2026-07) established the starting
point, which is higher than assumed. We already ship, and YaCy has no principled
equivalent of:

- **Per-field BM25 with tuned field weights** (`searchindex/ranking_weights.go`:
  title 6, anchors 4, headings 3, url 2, body 1 — practical BM25F range,
  Robertson & Zaragoza CIKM 2004), a weighted disjunction per query term.
- **Sequential Dependence Model, ordered feature** (`searchindex/sdm_bigrams.go`:
  adjacent-pair phrase clauses, Metzler & Croft SIGIR 2005).
- **Multiplicative static priors** (`searchlocal.go` `hostRankScorer`:
  `score ×= 1 + wHost·rank(host) + wFresh·2^(−age/180d) + urlPrior(path)`; host
  block-rank, freshness decay, saturating URL-length — Kraaij-Westerveld-Hiemstra
  SIGIR 2002, Zaragoza TREC-13).
- **Rank fusion** across local/remote/web sources (`searchcore/rrf.go`, RRF k=60,
  Cormack SIGIR 2009), **RM3 pseudo-relevance feedback** (`pseudo_relevance.go`,
  drift-guarded), **MMR diversification + SimHash near-dup drop + host crowding
  cap** (`searchcore/mmr.go`, `diversity.go`), SymSpell spellcheck, query-biased
  snippets, and an **offline NDCG@k evaluation harness** (`searcheval`:
  `Judgment{Query, Relevant map[url]grade}`, `NDCG(results, relevant, k)`,
  `Evaluate → Report`, `PseudoJudgments`).

YaCy, by contrast, is a hand-tuned linear combination of Solr field boosts plus
Block Rank and date boosts, with operator-editable ranking profiles: no term
dependence, no content-quality model, no learned calibration, no principled
cross-source fusion. We are already ahead; the question is how to extend the lead
without violating KISS.

Three facts about our architecture gate what is worth building:

1. **Layer split.** Bleve fields are stored with `Store=false`; the full document
   lives in a Go-side document map. Inside the **searchindex** layer, per hit, we
   have the full body, headings, anchors, url, and **term-vector positions**
   (`IncludeTermVectors=true`) and the per-clause `Explain` tree. The **searchcore**
   reranker/fusion layer sees only `Result{Title, Snippet≤320, URL, Score, Host,
   Date}` — no positions, no full body — so today's snippet-scoped proximity in
   `lexical_rerank.go` is weak. Any real proximity, quality, or per-field feature
   work must live in searchindex, or be plumbed up from it.

2. **Bleve hides k1/b.** The scorer's BM25 parameters are fixed (~1.2/0.75) and not
   exposed through the mapping API. BM25F, BM25+/BM25L, and per-field b/k1 all
   require forking the scorer — a new-dependency-grade change — and Trotman,
   Puurula & Burgess (ADCS 2014) show these are marginal once tuned. Low return
   for a large, ADR-grade cost.

3. **No labels persist.** `searcheval` runs against a handful of hand-authored
   judgments; no qrels or click log is stored. Gradient-boosted learning-to-rank
   (LambdaMART/GBDT) wants ~136 features and thousands of graded pairs, and the
   only pure-Go path (`leaves`) does inference, not training. It is infeasible
   here. The realistic learned path over our ~7 existing weights is **coordinate
   ascent** (Metzler & Croft SIGIR 2007; RankLib), which is "most effective when
   the number of features and examples is small."

## Decision

Build **YagoRank**: a learned log-linear ranker whose weights are fit to NDCG
against a persisted judgment set, layered on the retrieval and fusion stack we
already have. Pure-Go, CPU-only, no new dependency. Delivered as ordered slices,
each under the standing per-slice gate.

The scoring model:

    retrieval(d) = Σ_field boost_field · BM25(field)              # unchanged core
    prior(d)     = 1 + θ_host·rank + θ_fresh·2^(−age/180d)
                     + θ_url·urlDepth + θ_qual·quality(d)         # quality is new
    rerank(d)    = Σ_i θ_i · f_i(d)   over the top-K, features f_i:
       f1  normalized BM25F retrieval      f7  min-window proximity (doc-scoped, new)
       f2  SDM ordered-bigrams (shipped)   f8  log(1+hostrank)
       f3  SDM unordered-window (new)      f9  freshness
       f4  coverage ratio (title)          f10 url-depth prior
       f5  coverage ratio (body)           f11 content-quality prior (new)
       f6  all-terms / phrase in title     f12 anchor-text match (shipped, ADR-0029)

Every θ — the field boosts, the prior coefficients, and the rerank weights — is
fit offline to maximize mean NDCG@10 over the judgment set, replacing today's
hand-set constants. Fusion across sources stays RRF; diversification stays
MMR + SimHash + host cap.

Ordered slices:

- **RANK-00 — judgment set (qrels).** Persist graded `query→{url:grade}` judgments:
  operator-curated entries plus implicit judgments mined from the query-activity
  log honoring the existing privacy modes. Reuses the `searcheval.Judgment` /
  `PseudoJudgments` shapes. This is the prerequisite for any learning.

- **RANK-01 — coordinate-ascent learner.** A pure-Go coordinate-wise line search
  (~150–250 LOC, offline, zero query-time cost) that fits the existing
  `RankingWeights` and prior coefficients to mean NDCG@10 over the judgment set and
  writes them back into the live ranking profile. This alone improves ranking —
  the current weights are guesses — before any new feature exists.

- **RANK-ENABLER — expose per-field BM25 + positions.** Plumb per-field BM25
  sub-scores (via `req.Explain`) and matched-term positions (via
  `IncludeLocations`; term vectors are already on) from searchindex into
  searchcore Results, so proximity and coverage are computed over the document,
  not the 320-rune snippet. Unblocks the next two slices and richer features.

- **RANK-02 — content-quality prior.** Deterministic, language-agnostic per-document
  features at index time (stopword fraction and coverage, term-distribution
  entropy, average term length, document length — Bendersky, Croft & Diao WSDM
  2011; symbol/word ratio, short-line fraction — FineWeb, arXiv:2406.17557) folded
  into the existing `1 + Σ prior` multiplier. Reported ~+3–6% MAP over an SDM
  baseline; index-time O(length), query-time O(1). Its weight is fit by RANK-01.

- **RANK-03 — complete SDM.** Add the unordered-window (#uwN) feature as a
  document-scoped body scan over matched-term positions (reusing the body sweep
  already used for `near` and snippets), complementing the shipped ordered
  feature. SDM as a whole is ~+7–15% MAP on web collections (≈0 on newswire); its
  weight is fit by RANK-01.

## Consequences

- Ranking becomes **fit to measured relevance** rather than hand-tuned: the ~7
  weights we ship today, and the two new priors/features, are chosen by an offline
  NDCG optimizer over real judgments. This is the structural difference from YaCy,
  whose weights are hand-set permanently.
- All learning is **offline and pure-Go**; query-time cost rises only by the
  document-scoped proximity/quality features (both cheap, and computed in the
  searchindex layer where the body already resides). No GPU, no model runtime, no
  new dependency, no wire change — the RWI and every peer-protocol surface are
  untouched, so swarm interop is unaffected.
- The judgment set (RANK-00) becomes a durable asset: it also regression-guards
  ranking changes, since every later slice is scored against it.
- The reranker stops being snippet-scoped once RANK-ENABLER lands, which also opens
  the door to a larger feature set later without further plumbing.
- Retrieval quality still depends on a good judgment set; a small or skewed set
  limits how much coordinate ascent can help. Mitigation: seed with curated
  queries and grow from implicit signal.

## Alternatives considered

- **BM25F, BM25+/BM25L, or per-field/global b/k1 tuning.** Rejected. Bleve's scorer
  hides k1/b, so all of these require forking it (ADR-grade), and Trotman 2014
  shows the gain is marginal once anything is tuned. Our per-field boost vector
  already approximates BM25F's intent, and its weights are learned here anyway.
- **LambdaMART / GBDT learning-to-rank.** Rejected for now. It needs many features
  and thousands of labels we do not have, and pure-Go `leaves` only does inference
  — training would need Python/LightGBM, off-limits in the pipeline. Coordinate
  ascent captures most of the benefit at a fraction of the complexity and fits our
  existing linear model directly; GBDT can be revisited only if a large labeled
  corpus ever materializes.
- **xQuAD / PM-2 explicit diversification.** Rejected. They beat MMR only by
  consuming a query-intent/aspect model we do not have, and our SimHash near-dup
  drop plus host-crowding cap already deliver the user-visible diversity wins in a
  small federated corpus.
- **Query segmentation / phrase detection.** Rejected as marginal and risky: the
  literature finding is "in-doubt-without" (leave a query unsegmented when
  unsure), and our SDM bigrams already capture most of the value cheaply.
- **Replacing RRF with score-based fusion (CombSUM/CombMNZ) across sources.**
  Rejected. RRF sidesteps the incomparable-score problem between local BM25,
  remote peers, and web fallback, and beats score fusion in the fusion literature;
  the only defensible tweak (z-score/ISR normalization on the remote side) is
  marginal and out of scope. RM3 and MMR likewise stay as they are.
