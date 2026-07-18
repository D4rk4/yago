# 0047. Separate morphology recall from positional ranking evidence

Date: 2026-07-15

## Status

Accepted

## Context

Yago needs multilingual inflectional recall without treating an analyzer match
as proof that a document contains the user's exact words in the intended order.
Snowball analyzers improve recall by conflating forms, but a stem is not a lemma
and can also conflate words that should carry different ranking evidence.
Removing a stop word can additionally erase the distance that the user placed
between the remaining requirements.

Historical Yandex publications describe useful evidence boundaries rather than
the current Yandex ranking formula. The 2003 morphology paper distinguishes
inflectional lemmatization from stemming. The ROMIP-2004 paper describes word
positions, relevant passages, word order, and smoothly decreasing agreement
between observed and query distances. The ROMIP-2006 experiment combines
lemmatized BM25-like evidence with adjacent, one-gap, and reverse-order pair
features, while warning that its collection differs materially from the web.
Yago does not copy the papers' coefficients or claim that these mechanisms
describe current Yandex production ranking.

The Sequential Dependence Model independently establishes adjacent ordered and
unordered query-term pairs as general information-retrieval evidence. Yago uses
that open model shape together with its own fixed bounds and confidence policy.

The relevant primary sources are:

- [A fast morphological algorithm with unknown word guessing induced by a dictionary for a web search engine](https://download.yandex.ru/company/iseg-las-vegas.pdf)
- [Yandex at ROMIP-2004: some aspects of full-text search and ranking](https://download.yandex.ru/company/experience/romip2004/romip2004_aspects.pdf)
- [Yandex text-ranking algorithm at ROMIP-2006](https://download.yandex.ru/company/03_yandex.pdf)
- [A Markov Random Field Model for Term Dependencies](https://ciir.cs.umass.edu/pubfiles/ir-387.pdf)
- [Current Yandex word-form query behavior](https://yandex.ru/support/search/ru/query-language/search-context)
- [Current Yandex ranking principles](https://yandex.ru/company/rules/ranking)

## Decision

1. Each distinct normalized raw query requirement keeps a stable identity and
   its ordinal in the original query. The selected analyzer may remove a
   requirement from retrieval, but it does not renumber the remaining
   requirements or collapse their expected gaps.
2. Candidate retrieval remains fielded BM25 over the document's selected
   analyzer plus the exact-word branch. Existing Snowball analyzers are
   morphology recall, not lemmatizers and not exact-surface evidence.
3. The leading ten local candidates reuse the existing bounded stored-document
   evidence scan. No Bleve term-vector query, second document scan, corpus scan,
   external request, new index field, or new stored position list is added.
4. Every scanned occurrence can satisfy at most one raw requirement when two
   requirements collapse to the same analyzer token. Exact-surface locations
   remain separate from analyzer-equivalent locations. Matching never crosses a
   title, heading, anchor, body field, or repeated field value.
5. Local adjacent-pair proximity uses two confidence levels. An exact-surface
   pair contributes `1`. A non-fuzzy analyzer-equivalent pair contributes
   `0.5`. The unordered channel requires both occurrences within the existing
   eight-token window. The ordered channel requires their forward distance to
   equal the original raw-query ordinal gap. The best same-field evidence is
   retained. Fuzzy edit matches receive no analyzer-equivalent proximity credit;
   exact words in a fuzzy request can still provide exact evidence. A standard
   no-stem analyzer and a repeated identical raw requirement create no analyzer
   pair credit. A single unsegmented CJK requirement may contain several
   analyzer-position groups, each containing unigram and bigram alternatives; a
   coherent contiguous path contributes the same lower `0.5` analyzer confidence.
   Adjacent exact CJK query words retain exact confidence when their byte offsets
   touch, even though the overlapping bridge bigram occupies an internal analyzer
   position.
6. Relaxed-only candidates must independently pass a binary exact-surface
   quorum within one bounded field value. The quorum is an admission rule, not
   an IDF-, term-frequency-, or passage-selection score. An uninspected or
   deadline-interrupted relaxed-only candidate fails closed. Every mixed
   alphanumeric requirement is additionally mandatory in retrieval and must
   occur exactly inside the accepted passage; morphology and fuzzy evidence
   cannot substitute for a model, product, or protocol identifier.
7. Quoted-phrase analysis preserves the analyzer's token-position gaps, so an
   analyzer-removed stop word does not turn a phrase into false adjacency. The
   stored tokenizer keeps straight, curly, and full-width internal apostrophes
   inside a word, preserving possessives in long statement queries.
8. The final merged lexical reranker inspects at most 50 results. For exact
   surface positions, each adjacent retained pair receives forward gap
   agreement `1 / (1 + abs(observedGap - originalGap))`; the best occurrence
   supplies the pair value, so repetition does not accumulate credit. Stored
   positions are reused when available. Peer, web, and legacy-RWI rows analyze
   bounded title, snippet, and decoded-URL text through one compatible registered
   analyzer before ranking; structural matching is reserved for invalid input,
   unavailable analyzer infrastructure, or an unfinished deadline-bounded row. A
   web row must visibly contain every mixed alphanumeric identifier before its
   bounded term quorum is considered. This small refinement does not alter the
   existing binary ordered evidence.
9. Exact and analyzer-equivalent confidence values remain fixed evidence policy.
   The bounded ordered, unordered, lexical-blend, and original-gap score
   coefficients belong to the persisted YagoRank profile and are independently
   tunable in the admin console. Learned evidence keeps its versioned schema and
   held-out relevance, safety, and p95 latency gates in
   [ADR-0043](0043-pure-go-evidence-learning-to-rank.md).
10. Dictionary lemmatization remains excluded from this architecture. The
   optional Chinese and Japanese surface segments in ADR-0054 and ADR-0055 add
   word boundaries, not lemmas, readings, or inflectional analysis. Full
   lemmatization is not a latency-neutral serving signal: it requires additional
   language-specific data, licensing and dependency review, a changed index
   stream, a full reindex, and representative multilingual judgments. Yandex
   MyStem is additionally rejected because its
   [license](https://yandex.ru/legal/mystem/ru/) does not permit using it to
   provide an analogous service.

## Cost and abuse bounds

| Evidence | Request-time work | Index-time and storage work | Abuse and correctness bound |
| --- | --- | --- | --- |
| Analyzer recall | Existing bounded analyzer fan-out during BM25 retrieval | Existing single analyzer stream per document | Exact-word clauses preserve identifiers; analyzer matches are not promoted to exact evidence |
| Stored exact and word-form pairs | One pass already required for evidence over at most ten local candidates, then a linear adjacent-pair walk | None | One occurrence per raw requirement; lower confidence for analyzer variants; fuzzy variants excluded |
| Relaxed passage admission | Reuses the same leading-candidate scan | None | Exact-surface quorum, mandatory exact mixed identifiers, one field value, bounded token span, fail-closed tail |
| Exact original-gap agreement | Linear two-pointer walks over capped positions for at most 50 merged results; visible-text fallback has no I/O | None | Forward adjacent query pairs only; best match rather than repeated-hit accumulation |

The sequential microbenchmarks for the exact-gap refinement retained the same
allocation count and showed no material regression, but they are not an
interleaved before/after experiment, a relevance evaluation, or an end-to-end
p95 proof. The stored-evidence and complete search latency gates remain the
acceptance criteria.

## Patent boundary

The implementation uses plain token order and distance only. It does not build
candidate-passage TF/IDF or minimum-coordination scores, semantic list or header
distance rules, center-token hit sets, query-token-density values, document
density aggregates, lead-position probability distributions, or influence
kernels. The detailed engineering screen and its limits are recorded in
[ADR-0048](0048-bounded-google-yandex-ranking-research-disposition.md).

## Consequences

- Exact compact wording ranks above compact analyzer-equivalent wording, which
  ranks above scattered analyzer-equivalent wording when other evidence is
  equal.
- `mouse for gaming` retains a two-position expected gap after stop-word
  filtering instead of receiving artificial adjacency credit.
- Russian and other supported inflections can contribute bounded proximity
  without being represented as exact word forms.
- Chinese, Japanese, and Korean query text contributes grouped unigram/bigram
  evidence with or without whitespace. A coherent analyzer-position path is
  required, so alternatives from scattered spans cannot assemble a false
  contiguous chain. Chinese and Japanese dictionary segments are optional
  ranking evidence, and equal-code-point Traditional/Simplified Chinese forms
  share canonical terms; this remains bounded analysis rather than full
  morphology.
- Arabic uses bounded normalization and light stemming. Hebrew currently has
  normalized exact-word proximity but no morphology analyzer.
- Irregular forms that the selected analyzer does not conflate are outside the
  supported morphology contract. They retain exact and bounded-fuzzy behavior
  and are not repaired with query-specific synonyms or an unvalidated dictionary.
- Historical publications support the evidence shape only. They do not establish
  current vendor behavior or authoritative production weights.
