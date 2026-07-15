# 0027. Expand single-word swarm queries into corpus-observed inflections

Date: 2026-07-06

## Status

Accepted

## Context

ADR-0026 gave the **local** index real per-language morphology: a document and a
query are run through the same Snowball analyzer, so a local search for
`черногория` also matches the inflected `черногории` and `черногорию`. The
**swarm** search has no such property. A remote YaCy search addresses peers by
DHT word hash: `yagomodel.WordHash` hashes the exact, lowercased word, and the
hash preserves nothing about morphology — peers store postings under the exact
surface form and do not stem. So a swarm query for `черногория` reaches only the
peers holding that exact word; a peer that indexed a page containing solely
`черногории` is never consulted. We must not break this wire contract: the hash
is YaCy's addressing primitive and interop with stock YaCy peers depends on it
(the compatibility rule and ADR-0018).

The recall fix therefore has to happen **client-side, before hashing**: turn one
query word into a small set of surface forms, hash each as an ordinary exact-word
hash, search all of them, and fuse the passes. Every hash on the wire stays a
plain YaCy word hash, so peers — ours or stock — need no change.

The open question is where the surface forms come from. The first implementation
hand-wrote per-language ending tables (a Russian list `и/ю/е/у/ой/…`, an English
list, stem-suffix strip rules per language). That was rejected in review for the
obvious reason: **it does not scale to the languages of the world.** Authoring
and maintaining an inflection table per language is exactly the work the Snowball
stemmers already encode, done worse and by hand, and it silently does nothing for
every language without a table.

Verified findings:

- **A primary stemmer per script is a bounded generator in reverse.** ADR-0026
  registers 21 complete language normalization or stemming analyzers plus the
  CJK bigram analyzer. A single query word is too short for reliable language
  identification, so the swarm expander uses one deterministic primary analyzer
  for each supported script: English for Latin, Russian for Cyrillic, Arabic for
  Arabic, and Hindi for Devanagari. Running the node's own indexed vocabulary
  through that analyzer groups common forms by stem:
  `черногория`, `черногории`, `черногорию` all reduce to `черногор`; `running`
  and `runs` to `run`. The set of surface forms sharing a stem **is** the
  corpus's observed surface variants of that stem — derived from data, not from
  a hand table. CJK and scripts without a selected morphology analyzer retain
  the normalized source word unchanged. Other languages that share a script do
  not gain their own morphology from this optional single-word swarm feature.
- **The vocabulary is already being swept.** SEARCH-14's SymSpell corrector and
  host authority already need a periodic pass over stored documents. The
  morphology expander needs the same title and extracted-text frequencies, so
  all enabled signals are collected in that one pass.
- **Corpus-grounded forms are wire-safe and cheap.** Because the forms come from
  words the node has actually indexed, each is a real word that hashes to an
  ordinary exact-word hash, and the expansion only ever emits forms that some
  peer plausibly holds — no speculative, never-indexed strings inflating the
  fan-out.

## Decision

1. **Corpus + stemmer expander (`internal/wordforms`).** `Expander` holds a
   `stem → surface forms` map and an injected stemmer. `New(vocabulary, stem)`
   groups a term→frequency vocabulary by stem, keeping each stem's most frequent
   forms; `Variants(word)` returns the query word first, then the other forms
   sharing its stem, de-duplicated and bounded (`maxVariants`). The package holds
   no language knowledge — the stemmer is a `func(string) string` dependency, so
   `wordforms` stays a pure leaf with no import of the index.

2. **Reuse a bounded ADR-0026 analyzer (`searchindex.StemWord`).** The index
   package exposes `StemWord`, which reduces a word with the deterministic
   primary morphology analyzer for its dominant Unicode script. CJK, a script
   with no selected stemmer, or a stop-filtered token folds to the normalized
   word unchanged. Query and vocabulary are therefore grouped consistently and
   no endings are hardcoded anywhere, without claiming language identification
   from one word.

3. **Shared corpus signal pass (`internal/yagonode`).** One completion-relative
   pass collects bounded authority citations, spelling frequencies, and, only
   when enabled, morphology frequencies. It feeds heap-backed Space-Saving
   synopses instead of retaining every distinct token. Morphology retains at most
   32,768 frequent terms; spelling retains at most 8,192. Tokens outside the
   searchable 4-through-32-rune range are rejected before SymSpell generates its
   quadratic delete variants. The rebuilt `Expander` is published through an
   atomic `Holder` for lock-free reads on the query path. The bounded vocabulary
   shares the corpus-signal vault checkpoint and is restored before listeners
   open. Enabling morphology when the last checkpoint has no morphology summary
   starts a replacement corpus scan immediately. A node without swarm morphology
   pays no extra vocabulary collection or separate scan.

4. **Opt-in, single-word only (`internal/searchremote`).** The remote searcher
   takes an `ExpandWord func(string) []string`. When it is wired and the query is
   a single word, the searcher runs the exact conjunctive search once per variant
   and fuses the passes by reciprocal rank (RRF, ADR sibling SEARCH-11), so a
   document indexed under any inflection contributes while the base form keeps top
   rank; the fused list is capped to the requested limit. Multi-word queries keep
   the exact search — expanding several words would multiply the peer fan-out.

5. **Settings parity.** The feature is off by default and controlled by
   `YAGO_SWARM_MORPHOLOGY`, with the matching runtime admin setting
   `swarm.morphology.enabled` (per the settings-parity rule in AGENTS.md); the
   environment variable is only the bootstrap default.

## Consequences

- A single-word swarm query gains corpus-observed inflection recall for the
  deterministic primary morphology analyzer of its script, using only forms the
  swarm plausibly holds, with no hand-maintained tables or new dependency.
- Wire compatibility is untouched: each variant is hashed as an ordinary
  exact-word hash, so stock YaCy peers answer normally and are unaware of the
  expansion.
- The feature is opt-in because expansion multiplies the per-word peer fan-out
  (one query word becomes up to `maxVariants` DHT searches); operators enable it
  when recall matters more than round-trips.
- Enabling morphology adds its bounded frequency synopsis to the existing shared
  corpus pass; it does not add another scan. The retained vocabulary and the
  structures built from it have fixed cardinality bounds instead of growing with
  every distinct page token. The next pass starts only after the coarse interval
  has elapsed from completion of the previous pass.
- Expansion quality tracks the local corpus: a stem the node has never indexed
  under more than one form yields only the base word, so a freshly seeded node
  expands little until it has crawled enough vocabulary.

## Alternatives considered

- **Hand-written per-language ending tables** (the first attempt): a Russian
  suffix list, an English list, stem-strip rules per language. Rejected — it
  duplicates, by hand and worse, the morphology the Snowball stemmers already
  encode, and it does nothing for any language without a table. It is the reason
  this ADR exists.
- **Prefix/common-stem grouping over the vocabulary** (no stemmer, group words
  sharing a long common prefix): language-agnostic and dependency-free, but it
  cannot conflate forms whose stem changes (suppletion, consonant mutation) and
  over-groups unrelated words that merely share a prefix. The stemmer already
  ships and does this correctly, so prefix grouping is a strictly worse floor.
- **Server-side stemming on peers** (peers store stemmed hashes): would make the
  swarm morphology-aware without client expansion, but it changes the DHT hash
  and breaks interop with every stock YaCy peer. Non-starter under the wire-
  compatibility rule.
- **Expanding multi-word queries too**: multiplies the fan-out combinatorially
  (each word's variants × the others) for marginal recall over the conjunctive
  exact search. Rejected for the current architecture: single-word expansion
  captures the common navigational case where inflection hurts most, while
  multi-word expansion would consume the bounded peer-search budget without a
  demonstrated held-out gain.
