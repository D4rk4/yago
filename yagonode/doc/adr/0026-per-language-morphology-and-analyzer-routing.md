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
word occur scattered across almost any long Russian document. Recovery no longer
queries those fields either; it uses bounded analyzer-consistent fuzzy terms.

The requirement is language-appropriate lexical recall across popular
languages: a query for `черногория` must also match the inflected
`черногории` and `черногорию` without matching an unrelated document, while
languages without concatenative inflection or an available analyzer must use
an honest bounded fallback rather than pretending to have morphology.

Verified findings (bleve v2.6.0, checked against the module source and the IR
literature):

- **Stemming is the available bounded recall mechanism.** Skipping stemming
  costs about 50% of mean average precision for Russian (Dolamic & Savoy,
  *Information Processing & Management* 2009). The pinned Bleve packages
  register 22 complete analyzers: `ar cjk ckb da de en es fa fi fr hi hr hu it
  nl no pl pt ro ru sv tr`. Twenty-one provide language normalization or
  stemming, while `cjk` provides character bigrams. Other Bleve language
  directories expose individual filters rather than complete analyzers. A
  quick check confirmed the Russian analyzer
  conflates `черногория`/`черногории`/`черногорию` to one stem and does not
  match Cisco networking text. Snowball conflation improves recall but is not
  lemma analysis or exact-surface evidence;
  [ADR-0047](0047-morphology-and-positional-ranking-evidence.md) keeps those
  ranking signals separate.
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
  per document, queried with an explicit per-request analyzer) and BM25 scoring
  (adopted in ADR-0026's sibling change, SEARCH-26). Query-time phrase clauses
  and term-vector locations are excluded from the interactive path because
  large-body measurements exceeded both the latency and memory budgets.
- **CJK bigrams have an explicit boundary.** Unicode Standard Annex #29 notes
  that reliable word boundaries for Chinese and Japanese require dictionary or
  equivalent tailoring. The current dependency-free CJK analyzer provides
  bounded overlapping-bigram recall, not lexical segmentation; it therefore
  does not guarantee that a one-character query matches inside a longer run.
  See [Unicode Text Segmentation](https://www.unicode.org/reports/tr29/).

Coverage for the languages the requirement calls out, each verified against a
scorch index:

| Language | Script | bleve analyzer | Morphology quality |
|---|---|---|---|
| Russian | Cyrillic | `ru` | Full Snowball stemming; inflections conflate. |
| Arabic | Arabic | `ar` | Normalization + light affix/article stemming (not root-pattern conflation). |
| Chinese | Han | `cjk` | Character-bigram recall and contiguous-chain evidence; no dictionary word segmentation, script conversion, or morphology. A one-character query is not guaranteed to match inside a longer run because the analyzer stores overlapping bigrams there. |
| Serbian | Latin / Cyrillic | `hr` (Serbo-Croatian) for Latin | Latin conflates inflections via `hr`. Cyrillic Serbian retains normalized exact-word recall but has no Serbian morphology because short queries route through Russian and standard analysis, while the Croatian stemmer expects Latin text. |
| Hebrew | Hebrew | **none** | No bleve or Snowball Hebrew stemmer exists; the supported behavior is no-stemming exact words plus bounded typo recovery. A dictionary analyzer is outside this dependency-free index architecture. |

The lesson from Serbian and Hebrew is decisive: **routing by script alone is
insufficient.** Cyrillic is not only Russian (also Serbian, Ukrainian,
Bulgarian, Macedonian) and Latin is not only English (German, French, Spanish,
Serbian-Latin, …). Distinguishing them requires content-based language
detection.

A production crawl exposed the persistence-side version of this problem.
ArtOfWar correctly serves Russian HTML as `text/html; charset=windows-1251`, but
does not declare `<html lang>`. The crawler already decoded those bytes through
`golang.org/x/net/html/charset`, whose label table follows the WHATWG Encoding
Standard, so extracted titles and text were valid Unicode. The later artifact
builder nevertheless replaced every missing language declaration with `en` and
copied it into the document, URL metadata, and every RWI posting. The node's
content detector could choose a Russian analyzer, but it could not repair the
stored language facet or exchanged metadata. Language ID in the Wild
(Caswell et al., arXiv:2010.14571) also shows that held-out language-ID scores
overstate precision on noisy web crawls, so an uncertain classifier must retain
a valid publisher declaration rather than blindly overriding it.

Language-detection libraries considered:

- **`github.com/abadojack/whatlanggo`** (MIT, pure Go, chosen): 84 languages,
  Cavnar–Trenkle trigram model, emits both the language and the Unicode script,
  with an `IsReliable` confidence flag. Small memory footprint. ~92% on
  document-length text (its weakness — short strings — does not matter because
  we run it on document bodies, not queries).
- **`github.com/RadhiFadlillah/whatlanggo`** at pinned revision
  `v0.0.0-20240916001553-aac1f0f737fc` (MIT, pure Go): the maintained fork
  already linked into `yagocrawler` by the pinned trafilatura extractor. The
  crawler declares that existing module directly for the same trigram model and
  reliability signal, avoiding a second language-model copy in its binary.
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
   script-agnostic **`standard_text`** analyzer (Unicode tokenizer + lowercase +
   NFKC, no stemming) so it still ranks on exact words and participates in
   bounded typo recovery.

2. **Index-time language detection.** After extraction, the crawler runs
   whatlanggo over at most the first 64 KiB of UTF-8 main text and resolves one
   ISO 639-1 value before building any artifact. Reliable content detection
   wins; a syntactically usable HTML `lang` value wins when content evidence is
   uncertain; the detector's best content result is used when no declaration
   exists; and `en` remains only the no-signal compatibility fallback. Resolving
   once keeps the document, URL metadata, and RWI postings consistent and bounds
   classifier work. The node independently selects the Bleve analyzer from
   stored text, using the resolved crawl language as its prior and the dominant
   Unicode script as its floor. Reliable `ku` content in Arabic script routes to
   the Central Kurdish `ckb` analyzer; Latin-script `ku` retains the exact-token
   fallback. It stores the analyzer name and routes the
   document through Bleve's `TypeField`. One analyzer per document, one set of
   field names — no per-language sub-field explosion.

3. **Query-time routing without query LID.** Determine the query's dominant
   Unicode script deterministically (standard library `unicode` tables). Build
   the field queries analyzed with the bounded candidates that serve that script
   (Cyrillic with `ru`, Arabic script with `ar`, `fa`, and `ckb`, and Han with
   `cjk`), OR-combined, alongside the exact `standard_text` clause so a proper
   noun still matches a document in any language. The query string itself is
   never language-identified.

4. **Bounded stored evidence.** Keep term vectors off. Candidate retrieval
   returns scores and document identities first; only the leading ten local
   results are then scanned for a morphology-aware snippet. The scan rejects
   unrelated tokens before invoking a language analyzer, caps stored positions,
   and uses a single-pass component lookup for CJK. Paging performs the same
   bounded enrichment on later visible rows without changing their order.

5. **Unify the in-memory fallback on scorch.** Replace the upside-down
   `NewMemOnly` index with an in-memory scorch index so the fallback honors
   BM25 (ADR sibling SEARCH-26) and the same analyzer routing and bounded
   stored-evidence behavior as the on-disk shards.

6. **Migration.** The indexed and stored analyzer scope is incompatible with
   the persisted mapping, so existing shards rebuild from the document store.
   The same rebuild retires legacy character-gram fields and term vectors. A
   sibling marker is written before any destructive migration and cleared only
   after the complete scan. A restart that finds the marker discards the partial
   index and repeats the full rebuild before serving. A legacy shard with no
   rebuild source keeps serving under its old mapping; a marked partial rebuild
   without a source fails closed.

## Consequences

- Russian, and every language routed to a registered complete analyzer, gains analyzer-based
  morphological recall; the `черногория` flood is replaced by bounded,
  analyzer-aware matching rather than merely suppressed.
- Legacy-encoded HTML without a language declaration retains its decoded
  content language consistently across local documents, facets, RWI postings,
  and YaCy URL metadata after the page is crawled again.
- Latin Serbian works through the Croatian analyzer; Cyrillic Serbian retains
  exact-word recall without Serbian morphology. Chinese, Japanese, and Korean
  use contiguous CJK bigram evidence;
  Arabic gains light stemming; Hebrew and unlisted languages degrade
  gracefully to normalized exact words plus typo recovery. CJK bigrams are not
  dictionary segmentation, and the standard Hebrew fallback is not
  morphological recall. CJK single-character substring recall is excluded: it
  requires a larger index format and full reindex, while request-time substring
  matching would violate the bounded retrieval path and lower precision.
- One new pure-Go, cgo-free, MIT dependency (`whatlanggo`); no cgo, no
  multi-hundred-megabyte model.
- Per-language routing means a shard resolves several analyzers, but each
  document still writes one analyzed token stream. Removing legacy gram fields
  and term vectors reduces index write amplification and persisted size.
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
