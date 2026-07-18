# 0027. Expand swarm queries into bounded analyzer-consistent inflections

Date: 2026-07-06
Amended: 2026-07-17

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

The recall fix therefore has to happen **client-side, before hashing**: turn each
query requirement into a small set of surface forms and hash each as an ordinary
exact-word hash. Every hash on the wire stays a plain YaCy word hash, so peers —
ours or stock — need no change. Multiword retrieval must preserve conjunction:
surface forms are alternatives for one original requirement, while every
original requirement remains mandatory.

The open question is where the surface forms come from. The first implementation
hand-wrote per-language ending tables (a Russian list `и/ю/е/у/ой/…`, an English
list, stem-suffix strip rules per language). That was rejected in review for the
obvious reason: **it does not scale to the languages of the world.** Authoring
and maintaining an inflection table per language is exactly the work the Snowball
stemmers already encode, done worse and by hand, and it silently does nothing for
every language without a table. Corpus-observed forms solve that problem after a
node has indexed enough text, but leave a fresh requester unable to address a
regular sibling form held only by a stock YaCy peer.

Verified findings:

- **A primary stemmer per script groups corpus observations.** ADR-0026
  registers 21 complete language normalization or stemming analyzers. Its
  separate CJK analyzers provide unigram/bigram recall and bounded lexical
  segmentation, not inflectional stemming. A single query word is too short for
  reliable language identification, so the swarm expander uses one deterministic
  primary analyzer for each supported script: English for Latin, Russian for
  Cyrillic, Arabic for Arabic, and Hindi for Devanagari. Running the node's own
  indexed vocabulary
  through that analyzer groups corpus-observed forms that the selected analyzer
  maps to one stem:
  `черногория`, `черногории`, `черногорию` all reduce to `черногор`; `running`
  and `runs` to `run`. These primary groups are derived from observed data, not
  from a hand table, but they do not claim complete morphology for every
  language sharing the script. CJK and scripts without a selected morphology
  analyzer retain the normalized source word unchanged. The cold-corpus
  supplement separately considers every applicable registered analyzer backed
  by exported Snowball rules; it does not infer one language from a short query
  word.
- **The vocabulary is already being swept.** SEARCH-14's SymSpell corrector and
  host authority already need a periodic pass over stored documents. The
  morphology expander needs the same title and extracted-text frequencies, so
  all enabled signals are collected in that one pass.
- **Corpus-grounded forms are wire-safe and cheap.** Every observed form is a
  word the node has actually indexed and hashes to an ordinary exact-word hash.
  Generated candidates use the same wire shape but may not exist on a selected
  peer, so the shared attempt and concurrency ceilings bound that speculation.
- **The pinned analyzer rules can produce a bounded cold-corpus supplement.**
  Sixteen registered analyzers expose their generated Snowball rule tables.
  A candidate assembled from those tables is retained only when running it back
  through the same analyzer produces the query word's stem. This derives regular
  forms from the index analyzer rather than a project-owned language list. The
  original stays first, attempts and outputs are capped, and the network planner
  applies stricter shared limits. ADR-0059 records the direct dependency use.

## Decision

1. **Corpus + stemmer expander (`internal/wordforms`).** `Expander` holds a
   `stem → surface forms` map and an injected stemmer. `New(vocabulary, stem)`
   groups a term→frequency vocabulary by stem, keeping each stem's most frequent
   forms; `Variants(word)` returns the query word first, then the other forms
   sharing its stem, de-duplicated and bounded (`maxVariants`). The package holds
   no language knowledge — the stemmer is a `func(string) string` dependency, so
   `wordforms` stays a pure leaf with no import of the index. The search-index
   boundary supplements those observations with up to 12 analyzer-verified
   surfaces, selected from at most 2,048 rule-table attempts. It retains every
   applicable rule-backed analyzer identity even when stemming leaves the query
   word unchanged or several analyzers produce the same stem, and round-robins
   proposals across those identities under the shared attempt cap. Each proposal
   must map back to the query stem under its proposing analyzer. Duplicate
   surfaces accumulate their distinct verifying analyzer identities, then one
   global order prefers more distinct-analyzer agreement, shorter edit distance
   and length difference, greater prefix retention and rule support, analyzer
   priority, and lexical order. The original remains first and the result stops
   at 12 surfaces.
   Rule-based generation accepts terms from four through 32 Unicode runes;
   shorter or longer terms return only their normalized base form.

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

4. **Opt-in, bounded grouped retrieval (`internal/searchremote`).** The remote
   searcher takes an `ExpandWord func(string) []string`. A single-word query runs
   the exact search once per retained form and fuses those passes by reciprocal
   rank. A multiword query keeps one unchanged exact conjunctive primary request,
   then uses YaCy index abstracts for morphology recovery. Abstract URL hashes
   are unioned across the forms of each original query requirement and intersected
   across requirements. Metadata is requested only for the resulting conjunction,
   and evidence and ranking continue to use the original query terms. This avoids
   the Cartesian product of variant combinations.

5. **Fixed work bounds.** The original form is always first. At most 12 forms
   are retained for one requirement, at most 20 forms are addressed across the
   complete multiword query, and at most two peers are contacted for one form.
   Observed and generated candidates are interleaved after every requirement
   receives its exact form, so neither source can consume the complete bound.
   Primary, abstract, and metadata work share the existing aggregate deadline,
   8 MiB peer-response budget, 1,024 metadata-row budget, 8,192 abstract-hash
   budget, and a ceiling of 32 actual HTTP attempts per query. Multiword
   speculative abstract jobs can consume at most 20 of those attempts and
   additionally share eight process-wide morphology slots inside the ordinary
   32-slot peer admission. Single-word variant passes and the later metadata
   cover use the total per-query and ordinary process-wide ceilings.

6. **Settings parity.** The feature is off by default and controlled by
   `YAGO_SWARM_MORPHOLOGY`, with the matching runtime admin setting
   `swarm.morphology.enabled` (per the settings-parity rule in AGENTS.md); the
   environment variable is only the bootstrap default.

## Consequences

- Single- and multiword swarm queries gain corpus-observed inflection recall for
  the deterministic primary morphology analyzer of each term's script. Common
  regular forms covered by the supported Snowball-rule analyzers can additionally be addressed before the
  requester has observed them, without a hand-maintained table. ADR-0059 owns the
  direct use of the already-pinned Snowball dependency.
- Wire compatibility is untouched: each variant is hashed as an ordinary
  exact-word hash, so stock YaCy peers answer normally and are unaware of the
  expansion.
- The feature is opt-in because it adds bounded per-form abstract work. Multiword
  work grows linearly with retained forms and never creates a request for each
  combination of forms.
- Abstract provenance is retained per peer, term, and URL. Metadata retrieval
  chooses a deterministic greedy cover of proven admitting terms and splits a
  peer only when no one selected term covers every remaining URL. A query-
  sensitive stock peer therefore receives the same exact surface hash that
  admitted each requested URL.
- Enabling morphology adds its bounded frequency synopsis to the existing shared
  corpus pass; it does not add another scan. The retained vocabulary and the
  structures built from it have fixed cardinality bounds instead of growing with
  every distinct page token. The next pass starts only after the coarse interval
  has elapsed from completion of the previous pass.
- Expansion quality still improves with the local corpus. The bounded
  Snowball-rule supplement does not infer suppletive or other analyzer-
  unconnected forms, and other languages still need observed vocabulary or a
  cooperating Yago peer's negotiated analyzer recall.

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
- **One primary request per multiword variant combination:** rejected because
  its Cartesian fan-out grows exponentially and spends the peer budget before
  useful rows can return. The grouped index-abstract plan provides OR-within,
  AND-across semantics with linear, capped work while retaining the exact
  primary request.
