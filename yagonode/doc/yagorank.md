# YagoRank

YagoRank is the node's results-ranking stack: a **learned log-linear ranker**
whose weights are fit offline to NDCG against a persisted judgment set, layered on
the retrieval and fusion machinery the node already runs. It is pure-Go, CPU-only,
carries no model runtime and no extra dependency, and changes nothing on the wire —
the RWI and every peer-protocol surface are untouched, so swarm interop is
unaffected.

The design and its rationale are recorded in
[ADR-0035](adr/0035-learned-log-linear-ranking-yagorank.md); this document
describes what is actually shipped and how to operate it.

## Why it exists

Java YaCy ranks with a hand-tuned linear combination of Solr field boosts plus
Block Rank and date boosts: no term dependence, no content-quality model, no
learned calibration. Its weights are set by hand and stay that way.

YagoRank keeps the same *shape* — a transparent linear model a human can read — but
makes every weight **fit to measured relevance** instead of guessed. That is the
one structural difference from YaCy, and it is deliberately modest: the goal is a
decisively better ranker that stays KISS, not a "slow monster" of learned
components nobody can explain.

## The scoring pipeline

A query flows through four stages. YagoRank spans the first three; the fourth is
presentation.

```
                       searchindex layer                     searchcore layer
 query ─▶ ① retrieval ─────────────────▶ ② priors ─▶ ③ fusion + rerank ─▶ ④ diversify ─▶ results
          per-field BM25 (bleve)          score ×=      RRF across            MMR + SimHash
          + SDM ordered bigrams           1 + Σ prior   local/remote/web      + host-crowding cap
```

1. **Retrieval — `searchindex` (bleve).** Each query term is a weighted
   disjunction over the fields (title, headings, anchors, url, body), so a hit's
   base score is the field-boost-weighted sum of per-field BM25. On top ride the
   **SDM ordered-bigram** phrase clauses (adjacent query-word pairs), which lift
   documents where the words appear as an ordered window.

2. **Priors — `searchlocal` (`hostRankScorer`).** The retrieval score is scaled by
   a multiplicative envelope of query-independent and proximity signals:

   ```
   final(d) = retrieval(d) × ( 1
              + urlPrior(path)          # fixed saturating URL-length prior, always on
              + wHost  · rank(host)     # host block-rank authority
              + wFresh · 2^(−age/180d)  # exponential freshness decay, half-life 180 days
              + wQual  · quality(d)     # content-quality prior            (RANK-02)
              + wProx  · proximity(d,q) # SDM unordered-window feature      (RANK-03) )
   ```

   Each `w·signal` term is added only when its weight is positive, so a profile
   that zeroes a prior pays nothing for it.

3. **Fusion + rerank — `searchcore`.** Local, remote-peer, and (optional) web
   results are merged with **reciprocal-rank fusion** (RRF, k=60), which sidesteps
   the incomparable-score problem across sources. A learning-free lexical reranker
   then gently reorders the merged top window by query-term coverage and global
   proximity — a tie-break, not an override (25% of the reorder key).

4. **Diversify — `searchcore`.** MMR relevance/novelty trade-off, SimHash near-dup
   drop, and a per-host crowding cap produce the final list.

## The learnable weights

Nine weights make up the live **ranking profile**
(`searchindex/ranking_weights.go`). Coordinate ascent fits all nine; the shipped
defaults are the starting point.

| Weight | Default | Stage | What it scales |
| --- | --- | --- | --- |
| `title` | 6 | retrieval | BM25 on the title field |
| `anchors` | 4 | retrieval | BM25 on inbound anchor text (ADR-0029) |
| `headings` | 3 | retrieval | BM25 on headings |
| `url` | 2 | retrieval | BM25 on the URL field |
| `body` | 1 | retrieval | BM25 on the body |
| `hostRank` | 0.3 | prior | host block-rank authority |
| `freshness` | 0.2 | prior | recency decay bonus |
| `quality` | 0.2 | prior | content-quality prior |
| `proximity` | 0.15 | prior | SDM unordered-window proximity |

The field boosts sit in the practical BM25F range (Robertson & Zaragoza, CIKM
2004); our per-field boost vector approximates BM25F's intent without forking the
scorer. The saturating URL-length prior (Kraaij, Westerveld & Hiemstra, SIGIR
2002) is a fixed always-on term, not one of the nine.

A ranking profile persisted before a given prior existed decodes that weight as
zero, so the prior stays off until the profile is re-saved or re-tuned — old
profiles keep working, they just do not benefit from the newer signals yet.

## The Sequential Dependence Model

YagoRank implements all three feature classes of the SDM (Metzler & Croft, SIGIR
2005), which is worth ≈+7–15% MAP on web collections:

- **T — unigrams.** Per-field BM25 over the individual query terms (the retrieval
  stage). Always present.
- **O — ordered window.** Adjacent query-word pairs scored as phrase clauses that
  ride the bleve query (`sdm_bigrams.go`), at a fixed 0.12 of the field weight.
  bleve can express an ordered phrase, so this is a query-time feature.
- **U — unordered window.** The fraction of adjacent query-word pairs whose two
  words co-occur within a small token window of the document, order-independent
  (`sdm_unordered.go`). bleve has **no** unordered-window operator, so this is
  computed by a body scan in the searchindex layer at result mapping and folded in
  as the learnable `proximity` prior — its contribution is therefore fit to NDCG
  rather than fixed at a constant.

This local, pairwise proximity is complementary to the searchcore reranker's
*global* all-terms minimum-window tie-break: a long query whose words cluster
locally in phrase-sized runs but span the whole document scores well on U and
poorly on the global window, and vice-versa.

## The content-quality prior

`quality(d)` is a deterministic, language-agnostic score in `[0,1]`
(`contentprior/`) computed at index/mapping time from the document text. It reuses
the feature family of the crawl-time content-quality gate (function-word fraction —
Bendersky, Croft & Diao, WSDM 2011; symbol and alphabetic ratios — FineWeb,
arXiv:2406.17557) but **grades** instead of rejecting: the crawl gate is a hard
boolean that a keyword-stuffed page can still clear (a handful of function words in
hundreds), and the graded prior demotes exactly those pages relative to clean
prose. Text too short to grade, or in an unsegmented script, scores the neutral 1.0.

## Learning: judgments → coordinate ascent → profile

All learning is **offline, pure-Go, and zero query-time cost**.

1. **Judgment set (qrels).** Graded `query → {url: grade}` judgments persist in the
   vault (`judgments/`): operator-curated entries plus a click-log importer
   (RANK-00). The set doubles as a regression guard — every ranking change is
   scored against it. (Mining implicit judgments continuously from query activity,
   RANK-00b, is designed but deferred.)

2. **Coordinate ascent.** `rankfit` runs a coordinate-wise line search over the
   nine weights, maximizing **mean NDCG@10** across the judgment set
   (`searcheval` provides the NDCG harness). This is the learned path the
   architecture actually admits — "most effective when the number of features and
   examples is small" (Metzler & Croft, SIGIR 2007) — as opposed to GBDT, which
   would need thousands of labels and a non-Go trainer.

3. **Preview, then apply.** An operator triggers a tune run from the admin console;
   it returns a **before/after NDCG preview and the proposed weights but does not
   auto-apply**. The operator reviews and writes the new profile explicitly.

## Admin surface

All four endpoints live under the ops listener (`:9090`), admin-authenticated:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/admin/v1/search/ranking` | GET / POST | read or replace the live ranking profile (the nine weights) |
| `/api/admin/v1/search/ranking/tune` | POST | run coordinate ascent over the judgment set; returns `{before, after, beforeNdcg, afterNdcg, rounds, improved}` — a preview, never auto-applied |
| `/api/admin/v1/search/judgments` | GET / POST / DELETE | manage the qrels judgment set |
| `/api/admin/v1/search/explain` | POST | per-result score breakdown: per-field BM25 sub-scores, the `quality` and `proximity` features, and the bleve explanation tree |

## What YagoRank deliberately does not do

These were considered and rejected in ADR-0035; the omissions are load-bearing, not
gaps:

- **No BM25F / per-field k1,b fork.** bleve hides the scorer's k1/b, so any of these
  means forking it (dependency-grade), and the gain is marginal once anything is
  tuned (Trotman, Puurula & Burgess, ADCS 2014). The learned field-boost vector
  already approximates BM25F.
- **No LambdaMART / GBDT.** It needs thousands of labels we do not have and a
  Python/LightGBM trainer that is off-limits in the pipeline; pure-Go `leaves` does
  inference only. Coordinate ascent captures most of the benefit for our small
  linear model. Revisit only if a large labeled corpus ever materializes.
- **No xQuAD / PM-2 diversification.** They beat MMR only with a query-intent model
  we do not have; SimHash near-dup drop plus the host-crowding cap already deliver
  the visible diversity wins in a small federated corpus.
- **No score-based fusion (CombSUM/CombMNZ) replacing RRF.** RRF sidesteps
  incomparable local/remote/web scores and beats score fusion in the literature.
- **No query segmentation.** Marginal and risky ("leave unsegmented when unsure");
  the SDM bigrams already capture most of the value.

## Code map

| Concern | Location |
| --- | --- |
| Ranking profile + weights, validation, defaults | `internal/searchindex/ranking_weights.go` |
| Per-field BM25 + term positions extraction | `internal/searchindex/hit_features.go` |
| SDM ordered bigrams (O) | `internal/searchindex/sdm_bigrams.go` |
| SDM unordered window (U) | `internal/searchindex/sdm_unordered.go` |
| Content-quality prior | `internal/contentprior/` |
| Multiplicative priors envelope | `internal/searchlocal/searchlocal.go` (`hostRankScorer`) |
| RRF fusion, lexical rerank, MMR, diversity | `internal/searchcore/` |
| NDCG evaluation harness | `internal/searcheval/` |
| Coordinate-ascent learner | `internal/rankfit/` |
| Judgment-set persistence | `internal/judgments/` |
| Admin endpoints (ranking, tune, judgments, explain) | `internal/yagonode/search_*_endpoint.go` |

## References

- Metzler & Croft, *A Markov Random Field Model for Term Dependencies* (SIGIR 2005) — SDM.
- Metzler & Croft, *Linear feature-based models for information retrieval* (Inf. Retr. 2007) — coordinate ascent.
- Robertson & Zaragoza, *Simple BM25 Extension to Multiple Weighted Fields* (CIKM 2004) — BM25F.
- Kraaij, Westerveld & Hiemstra, *The Importance of Prior Probabilities for Entry Page Search* (SIGIR 2002) — URL-length prior.
- Bendersky, Croft & Diao, *Quality-Biased Ranking of Web Documents* (WSDM 2011) — quality features.
- Cormack, Clarke & Büttcher, *Reciprocal Rank Fusion* (SIGIR 2009) — RRF.
- Trotman, Puurula & Burgess, *Improvements to BM25 and Language Models Examined* (ADCS 2014) — why not to fork the scorer.
