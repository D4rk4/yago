# 0026. Route documents to per-language analyzers for multilingual morphology

Date: 2026-07-06

## Status

Accepted

## Context

Every text field in the search index is analyzed with the English analyzer
(`en`). English stemming is a near no-op on other languages and the English
stop-word list removes nothing from them, so the index carries **no
morphological normalization for any non-English language**. The only
cross-language recall was a character-trigram sub-field matched with AND
semantics, which flooded ordinary queries (a Russian query for `черногория`
pulled in unrelated Cyrillic pages because the eight common trigrams of the
word occur scattered across almost any long Russian document — SEARCH-24
restricted that clause to the zero-result recovery path as a stop-gap).

The requirement is DuckDuckGo-level morphology across popular languages: a
query for `черногория` must also match the inflected `черногории` and
`черногорию`, while never matching an unrelated document, and the same must
hold for German, French, Spanish, Arabic, Chinese, Serbian, and the rest.

Verified findings (bleve v2.6.0, checked against the module source and the IR
literature):

- **Stemming is the mechanism.** Skipping stemming costs about 50% of mean
  average precision for Russian (Dolamic & Savoy, *Information Processing &
  Management* 2009). bleve ships analyzers for 32 languages: `ar bg ca cjk ckb
  cs da de el en es eu fa fi fr ga gl hi hr hu hy id in it nl no pl pt ro ru sv
  tr`. A quick check confirmed the Russian analyzer conflates
  `черногория`/`черногории`/`черногорию` to one stem and does not match Cisco
  networking text.
- **Character n-grams must never be a boolean AND.** A word's trigrams matched
  with AND over a whole document is only a candidate superset — Manning,
  Raghavan & Schütze (*Introduction to Information Retrieval*, 2008, §3.2.2)
  show `red*` run as `$re AND red` also matches `retired`. Correct designs
  score n-grams as weighted terms under BM25 (IDF suppresses common grams) or
  enforce contiguity with a positional phrase (Lucene `NGramPhraseQuery`), and
  use n=4–5 for ranking, trigrams only for substring candidates (McNamee &
  Mayfield, *Information Retrieval* 7(1-2), 2004).
- **Do not identify the language of a query string.** Query language
  identification plateaus in the low 80% range (Ceylan & Kim, ACL-IJCNLP 2009:
  82.7% on Yahoo! queries averaging 2.6 words); Elastic's own guidance is to
  detect language at index time (documents, >92% accurate) and, at query time,
  use the query's Unicode **script** as a fast, deterministic signal and fan
  out across candidates.
- **The HTML `lang` attribute is a prior, not ground truth.** Present on ~83%
  of pages and syntactically valid on >99% of those (Web Almanac 2022), but
  frequently wrong in content; content-based detection stays authoritative.
- **bleve mechanisms are present:** per-document analyzer selection via
  `TypeField` (verified: two document mappings, one field, the right analyzer
  per document, queried with an explicit per-request analyzer); BM25 scoring
  (adopted in ADR-0026's sibling change, SEARCH-26); positional
  `MatchPhraseQuery` (needs term vectors, currently disabled — quoted-phrase
  boosts silently match nothing on the scorch shards).

Coverage for the languages the requirement calls out, each verified against a
scorch index:

| Language | Script | bleve analyzer | Morphology quality |
|---|---|---|---|
| Russian | Cyrillic | `ru` | Full Snowball stemming; inflections conflate. |
| Arabic | Arabic | `ar` | Normalization + light affix/article stemming (not root-pattern conflation). |
| Chinese | Han | `cjk` | Bigram segmentation — correct for an uninflected language; no stemmer needed. |
| Serbian | Latin / Cyrillic | `hr` (Serbo-Croatian) | Latin conflates inflections via `hr`; Cyrillic Serbian is indistinguishable from Russian by script alone and needs content detection to route to `hr`. |
| Hebrew | Hebrew | **none** | No bleve or Snowball Hebrew stemmer exists; falls back to no-stemming (exact word + n-gram recall). Root-pattern morphology needs a dictionary analyzer (HebMorph-class), tracked as a follow-up. |

The lesson from Serbian and Hebrew is decisive: **routing by script alone is
insufficient.** Cyrillic is not only Russian (also Serbian, Ukrainian,
Bulgarian, Macedonian) and Latin is not only English (German, French, Spanish,
Serbian-Latin, …). Distinguishing them requires content-based language
detection.

Language-detection libraries considered:

- **`github.com/abadojack/whatlanggo`** (MIT, pure Go, chosen): 84 languages,
  Cavnar–Trenkle trigram model, emits both the language and the Unicode script,
  with an `IsReliable` confidence flag. Small memory footprint. ~92% on
  document-length text (its weakness — short strings — does not matter because
  we run it on document bodies, not queries).
- **`github.com/pemistahl/lingua-go`** (Apache-2.0): best short-text accuracy
  (79/81/97%) but the high-accuracy models hold ~1.5 GB of n-gram data in RAM
  with all languages loaded — unacceptable for a peer node meant to run on
  modest hardware.
- **CLD3 via cgo** (`jmhodges/gocld3`): needs a C++ toolchain and protobuf,
  breaking the pure-Go, cgo-free build.

## Decision

1. **Per-language analyzers (`internal/searchindex`).** Register the bleve
   language analyzers and a language→analyzer table. Each supported language
   maps to its analyzer (`ru`→`ru`, `de`→`de`, …); Serbian and Bosnian map to
   `hr` (Serbo-Croatian); Chinese, Japanese, and Korean map to `cjk`; a
   language with no analyzer (Hebrew and any unlisted language) maps to a
   script-agnostic **`standard`** analyzer (Unicode tokenizer + lowercase +
   NFKC, no stemming) so it still ranks on exact words and recovers through
   n-grams.

2. **Index-time language detection.** Detect each document's language from its
   extracted text with whatlanggo, using the crawl-time HTML `lang` attribute
   (`documentstore.Document.Language`) as a prior and tie-breaker and the
   dominant Unicode script as a floor. Store the resolved analyzer name on the
   document and route the document to the matching per-analyzer document
   mapping through bleve's `TypeField`. One analyzer per document, one set of
   field names — no per-language sub-field explosion.

3. **Query-time routing without query LID.** Determine the query's dominant
   Unicode script deterministically (standard library `unicode` tables). Build
   the field queries analyzed with the analyzers that serve that script (e.g. a
   Cyrillic query is analyzed with `ru` and the other Cyrillic-script
   analyzers; a Han query with `cjk`), OR-combined, alongside the exact
   `standard`-analyzed clause so a proper noun still matches a document in any
   language. The query string itself is never language-identified.

4. **Term vectors on.** Enable `IncludeTermVectors` on the text fields so
   positional queries work — this fixes the quoted-phrase boosts (silently
   broken on scorch today) and unlocks a future `NGramPhraseQuery` substring
   clause to replace the recovery-only trigram AND.

5. **Unify the in-memory fallback on scorch.** Replace the upside-down
   `NewMemOnly` index with an in-memory scorch index so the fallback honors
   BM25 (ADR sibling SEARCH-26) and the same analyzer routing and positional
   queries as the on-disk shards.

6. **Migration.** The mapping change is incompatible with the persisted
   single-analyzer mapping, so existing shards rebuild from the document store
   through the mechanism that already handles the pre-trigram migration; a
   shard with no rebuild source keeps serving under its old mapping.

## Consequences

- Russian, and every language with a bleve analyzer, gains real morphological
  recall; the `черногория` flood is replaced by precise, inflection-aware
  matching rather than merely suppressed.
- Serbian works through the Croatian analyzer once content detection routes it
  there; Chinese works through `cjk`; Arabic gains light stemming; Hebrew and
  unlisted languages degrade gracefully to exact-plus-n-gram recall until a
  dictionary analyzer is added.
- One new pure-Go, cgo-free, MIT dependency (`whatlanggo`); no cgo, no
  multi-hundred-megabyte model.
- The index grows: term vectors add positional postings, and per-language
  routing means a shard holds several analyzers' token streams. Both are
  bounded by the existing per-shard size policy (ADR-0025).
- Upgrading nodes reindex once from the document store.
- Query fan-out across a script's analyzers adds clauses; it is bounded by
  restricting to the query script's analyzers, not all languages, and the
  exact `standard` clause keeps proper-noun recall cheap.

## Alternatives considered

- **Per-language sub-fields with `multi_match`** (Elastic's documented
  pattern): a `body_en`, `body_ru`, … per language. Rejected for bleve because
  its `DisjunctionQuery` multiplies the score by `coord = matched/total`, so a
  document matching only its one language sub-field out of N is structurally
  penalized; `TypeField` with a single field name avoids the sub-field blow-up
  and the coord dilution.
- **Script-only routing** (Cyrillic→`ru`, Latin→`en`): no new dependency, but
  it mis-stems every non-dominant language of a script — German and
  Serbian-Latin would get English stemming, Serbian-Cyrillic Russian stemming.
  It fixes the reported Russian case but not the multilingual requirement, so
  it is at best a fallback, not the design.
- **`lingua-go` / CLD3**: rejected on memory footprint and cgo respectively
  (see Context).
