# 0028. Close the local-morphology and partial-word follow-ups as subsumed

Date: 2026-07-06

## Status

Accepted

## Context

SEARCH-11 shipped language-agnostic partial-word recall (task #77, the trigram
`<field>_gram` sub-field) and left four follow-ups open, recorded on the porting
ledger:

1. an **edge-ngram prefix** field (index-time only, query-analyzer override) for
   exact truncation matching (`зеленск` → `Зеленский`);
2. **query-time fuzzy** matching (auto-fuzziness + prefix, ≤2 edits) for typos and
   two-sided edit proximity;
3. an **OR-recall** variant of the trigram field for general morphology
   (`работать` ↔ `работает`), with flooding mitigation;
4. a **phase-2 unsupervised stemmer** (YASS/GRAS/Morfessor) or a BPE/SentencePiece
   custom token filter for precision-per-recall.

Since that list was written, three later slices changed the picture:

- **SEARCH-24** proved the trigram AND-clause floods ordinary queries (a Russian
  query for `черногория` pulled in unrelated Cyrillic pages because a word's
  common trigrams occur scattered across any long same-script document).
- **SEARCH-25** (ADR-0026) added real per-language Snowball stemming with
  content-language detection and per-analyzer routing: `работать`/`работает`
  conflate to one stem for Russian and for every language bleve ships an analyzer
  for, precisely and without flooding.
- **SEARCH-14** added query-time fuzzy zero-result recovery and a SymSpell
  corrector that proposes a "did you mean" from the indexed vocabulary;
  autocomplete already offers prefix completion from local titles.
- A production query for `псилобаты` showed that moving trigram conjunctions to
  recovery was insufficient: scattered grams admitted 47 unrelated pages and
  made the retry scan for several seconds. Recovery now excludes gram fields,
  requires every parsed term, and uses bounded analyzer-consistent edit distance,
  including adjacent transpositions: distance one, or two for terms of at least
  eight runes. Distance two requires four stable leading runes, fuzzy matching is
  disabled above 64 runes, and the retry has a 150 ms budget.

## Decision

Close SEARCH-11. The four follow-ups are dispositioned, not implemented as
written:

1. **Follow-up 3 (OR-recall general morphology) is subsumed by SEARCH-25.**
   Per-language stemming conflates inflections correctly at index and query time,
   which is strictly better than an OR of trigrams and carries none of the
   flooding SEARCH-24 documented. No trigram OR-recall variant will be built.
2. **Follow-up 4 (unsupervised stemmer) is subsumed for the 32 stemmed languages
   and deferred for the rest.** Snowball stemmers beat unsupervised YASS/GRAS for
   any language that has one; an unsupervised or dictionary stemmer only adds
   value for scripts with no analyzer (Hebrew and unlisted languages), which
   ADR-0026 already records as a separate, later follow-up. It is not part of
   SEARCH-11.
3. **Follow-up 2 (query-time fuzzy) is delivered** by the bounded zero-result
   recovery plus the SymSpell corrector. It is not enabled on the ordinary query
   path.
4. **Follow-up 1 (edge-ngram prefix field) is deferred.** With stemming,
   bounded fuzzy recovery, and title autocomplete all in place, a dedicated
   edge-ngram field adds marginal recall for truncated words at the cost of a new
   index field and a full reindex. If a future need for exact prefix matching in
   the ranked results (beyond autocomplete) is demonstrated on the SEARCH-16 eval
   harness, it can be added then as an index-time-only analyzer override.

## Consequences

- SEARCH-11's morphology intent is met by per-language stemming, with bounded
  fuzzy/SymSpell recovery for typos. The analyzer-scope schema migration retires
  legacy trigram fields and term vectors, reducing index write amplification
  and persisted size.
- Existing shards rebuild once from the document store when a rebuild source is
  available. No new dependency is introduced.
- The genuinely-open morphology gap is narrowed to unstemmed scripts (Hebrew and
  unlisted languages), tracked under ADR-0026's dictionary-analyzer follow-up, not
  under SEARCH-11.

## Alternatives considered

- **Build the edge-ngram prefix field now.** Rejected as premature: it duplicates
  autocomplete's prefix behaviour on the ranked path and needs a reindex for a
  recall gain that the eval harness has not shown is missing.
- **Keep SEARCH-11 open indefinitely** as a catch-all morphology tracker.
  Rejected: its base is shipped and every remaining item is either delivered
  elsewhere or a distinct, separately-tracked follow-up; leaving it open hides
  that the morphology work is effectively complete for supported languages.
