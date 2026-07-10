# 0040. Prune RWI top-k with bounded selection and gated early scan termination, not MaxScore

Date: 2026-07-10

## Status

Accepted

## Context

PERF-CPU-01 set out to add MaxScore dynamic pruning to the RWI top-k retrieval
path. MaxScore (Turtle & Flood 1995; Mallia et al., ECIR 2019) and its WAND/
block-max relatives are dynamic-pruning algorithms for **disjunctive (OR) top-k
ranked retrieval with additive per-term score contributions** (BM25-style): they
partition the query's posting lists into essential and non-essential sets by each
term's **maximum score contribution**, then skip documents that can only be
reached through non-essential terms and cannot beat the current heap threshold.

The node's RWI local search (`internal/documentsearch`) does not meet any of
those preconditions, which an adversarial review confirmed against the code:

1. **The join is conjunctive.** `keepDocumentsMatchingEveryTerm` keeps only
   documents present in *every* query term — a set intersection. MaxScore exists
   to avoid fully evaluating the disjunctive union; on an AND query the candidate
   set is already the intersection and every term is essential, so there is
   nothing to skip.
2. **The ranking is not an additive per-term score with upper bounds.**
   `documentRelevanceOrder` sorts by summed `occurrences`, then by `termSpread`
   (the span between the first and last query-term text positions — a value that
   needs both endpoints and does not decompose into independent per-term
   contributions), then by identifier. There is no per-term max-score to drive an
   essential/non-essential partition, and no stored document-frequency index to
   order terms by selectivity (the only per-word count, `RWIURLCount`, is itself a
   full scan).
3. **Per-term match totals are on the wire.** `totalMatchesPerTerm` becomes the
   `indexcount.<hash>` fields of the YaCy search response, which a peer uses for
   multi-term index-abstract negotiation. Computing it requires the full per-term
   traversal that MaxScore is designed to avoid, and breaking that count would
   break swarm interop.

Enabling real MaxScore would require re-architecting the path into a disjunctive,
additively-scored retriever with per-term score bounds and a document-frequency
index — a different engine, not an optimization, and one that would abandon the
current YaCy-faithful conjunctive semantics.

## Decision

Do not implement MaxScore/WAND/block-max for the RWI path. Instead ship the two
CPU/IO reductions that fit the conjunctive, span-ranked, count-reporting
architecture and preserve every result and wire field exactly.

1. **Bounded top-k selection.** Replace the full sort of the whole matching set
   with a `limit`-sized heap (`container/heap`, standard library) that keeps only
   the most relevant `limit` documents, then orders just those. Because
   `documentRelevanceOrder` is a strict total order (identifiers are unique), the
   top-`limit` prefix is unique, so the output is identical to ordering everything
   and truncating — down to the last tie-break — while the work drops from
   `O(N log N)` to `O(N log limit)`. An unbounded or all-encompassing limit falls
   back to the full sort.

2. **Reporting-gated scan early termination.** A per-term scan already stops
   *keeping* matches at the `matchesPerTerm` cap but kept scanning to the end only
   to finish counting `totalMatchesPerTerm`. When that exact count is not read
   downstream, the scan may now stop at the cap, skipping the tail of a long
   posting list. This is opt-in through `allowEarlyTermination`, **off by default**
   so a scan stays exhaustive and wire counts exact unless a caller proves the
   totals are unused: the local searchcore path (which reports only the join
   total) enables it unconditionally, and the peer endpoint enables it only for
   requests that ask for no index abstracts. Abstract-bearing peer requests keep
   the exhaustive scan, so `indexcount` stays exact.

## Consequences

- RWI search keeps its exact results and its YaCy-compatible response, including
  `joincount` and the per-term `indexcount`/`indexabstract` keys; a wire-
  conformance test asserts `indexcount` stays exact even when the kept join is
  capped far below the true match count.
- The dominant per-query cost — decoding and filtering every posting of a common
  term — is cut on the local and no-abstract paths by stopping at the cap;
  abstract negotiation between peers is unchanged and pays the full scan, as it
  must.
- The default-off, prove-it-is-safe gating means a new caller that forgets to opt
  in is merely slower, never wire-incorrect.
- No new dependency: the bounded selection uses `container/heap`.

## Alternatives considered

- **Textbook MaxScore/WAND/block-max.** Rejected as architecturally inapplicable
  for the three reasons above; it would change wire-visible counts and presupposes
  disjunctive, additively-scored retrieval the RWI path does not perform.
- **Rarest-term-first intersection with point probes** (scan only the smallest
  posting list, probe the rest by key). A natural conjunctive optimization, but it
  computes no full per-term totals, so it breaks the wire `indexcount`; rejected
  for the reported path.
- **Short-circuiting the AND when a term yields zero matches.** Safe only under the
  no-abstract reporting mode and marginal in benefit there; folded into the same
  gate rather than treated as a separate mechanism.
