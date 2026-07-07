# Lexical ranking research & implementation (SEARCH-38)

Operator request: «реализуй нормальное адекватное ранжирование». The audit
showed ranking exists (BM25 scoring, field boosts, RRF fusion, MMR diversity,
lexical rerank) but three research-backed signals were missing and one was
disabled by default. Research sources: arxiv/SIGIR/TREC listed per section.

## What the pipeline already had (audit)

BM25 scoring in bleve (SEARCH-26); query-time field boosts over
title/headings/anchors/body/url; RRF fusion k=60 over local+peer lists
(Cormack et al., SIGIR 2009 — correct for incomparable score scales); lexical
rerank (coverage+proximity over title+snippet, w=0.25, window 50); MMR λ=0.7 +
host-crowding cap + SimHash dedup; RM3 pseudo-relevance feedback.

## What was missing → what shipped

1. **Term dependencies (SDM)** — Metzler & Croft, SIGIR 2005: unigram /
   ordered-window / unordered-window at 0.85/0.10/0.05. Shipped as automatic
   adjacent-pair phrase boosts over title and body at 0.12 of the field weight
   (≈0.10/0.85), the recommended cheap approximation on an inverted index.
   Boytsov (arXiv:2012.08020) shows BM25+proximity+fields+fusion beats early
   BERT rerankers on MS MARCO docs — this stack, exactly.
2. **Entry-page URL prior** — Kraaij, Westerveld & Hiemstra, SIGIR 2002:
   URL form alone puts >70 % of entry pages at rank 1. Shipped as the
   saturating static feature from TREC-13 (Zaragoza): `0.1·20/(20+len(path))`
   folded into the post-retrieval multiplier.
3. **Freshness prior** — Li & Croft, CIKM 2003 time-based models. Shipped as
   `Freshness·2^(−age/180d)` on dated documents; undated documents are not
   punished. Default weight 0.2.
4. **Host authority was OFF** — the YBR block-rank multiplier existed but
   `HostRank` defaulted to 0. Defaults now enable it at 0.3.
5. **Field weights** moved toward the TREC-13 practical range (Robertson &
   Zaragoza, CIKM 2004): title 6, anchors 4 (anchor text is what *other*
   pages call this one), url 2, headings 3, body 1.

Final local score:

```
bm25f-ish(title·6, headings·3, anchors·4, body·1, url·2)
  + SDM bigram phrase boosts (0.12·field)
→ score ×= 1 + 0.3·hostRank + 0.2·2^(−age/180d) + 0.1·20/(20+pathLen)
→ RRF(k=60) fusion with peers → lexical rerank → MMR/host-cap/dedup
```

## Deliberately not done

- True pre-saturation BM25F (single composite weighted field) — needs an
  index-format migration; the query-time boost approximation is the standard
  Lucene/bleve practice and the delta is small at these scales.
- Unordered-window SDM feature — no cheap bleve operator; ablations attribute
  most of the gain to the ordered feature.
- Tuned convex score fusion (Bruch et al., arXiv:2210.11934) — beats RRF only
  with training queries, which a self-hosted node does not have.
