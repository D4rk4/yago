# 0070. Bound analyzer-scoped search with complete lexical candidates

Date: 2026-08-27

## Status

Accepted

Amends [ADR-0069](0069-populate-active-search-mappings-before-readiness.md).

## Context

Release v0.0.48 populated every active Scorch mapping before readiness, but
nine new production queries still exhausted the 1.8-second interactive budget.
An immediate repeat was fast. A read-only copy of the exact production index
showed that the query terms themselves covered between 158 and 34,824 content
postings. The analyzer-selection clauses covered about 491,699 postings for
every query even though those clauses have zero ranking weight.

Bleve v2.6.1 can stop an unscored query after a requested number of hits. Its
BM25 path has no block-maximum metadata, MaxScore, or WAND scorer and therefore
scores every match. Bluge v0.2.2 has the same architectural limitation. Tatami
v0.3.0 has block-max WAND, but its query analysis, field scoring, context,
delete durability, and storage contracts do not match Yago. Adopting it would
also add an immature third-party runtime dependency and a second search model.

An arbitrary prefix of a posting list is not acceptable. On a disposable
100,000-document corpus, an unscored prefix retained all title-dominant results
but none of the deliberately late body-dominant reference results. A direct
impact heap retained those references, but a 560,000-document broad-term burst
had insufficient four-CPU margin and no persisted upper bound that could prove
an omitted document unable to enter the result page.

Every analyzer-scoped lexical match must also match at least one positive
content clause. The complete union of those clauses is therefore a safe
candidate superset. Analyzer scope, required-term conjunctions, relaxed-term
minimums, fuzzy matching, exclusions, expansion scoring, and domain narrowing
can remain authoritative in the unchanged final query.

On the production copy, the complete positive union contained 148 to 3,277
documents for eight failed queries and 29,871 for `economics`. Restricting the
original query to each complete set preserved all 100 reference identities.
Warm-copy candidate collection took 1.7 to 23.3 milliseconds and exact
reranking took 3.8 to 112.0 milliseconds. Separate deterministic fixtures prove
order, score, and total equality. These measurements prove the corpus relation
and implementation choice, not production capacity.

A 200-request mixed burst on four processors demonstrated that Bleve's existing
eight-shard fan-out already consumes the useful parallelism of one process.
Unlimited outer work reached p99 10.851 seconds. With one admitted disk read,
individual service time was p50 27.2 milliseconds, p95 240.4 milliseconds, and
p99 258.9 milliseconds, while queue-inclusive p99 for all 200 requests was
10.510 seconds. One process therefore cannot satisfy that burst inside the
interactive budget.

## Decision

For a disk search with analyzer-scoped mappings, first execute an unscored
disjunction of every distinct positive lexical requirement across the same
text, URL, language, dictionary, and fuzzy analyzers. Do not include analyzer
scope, exclusions, domain constraints, or optional expansion terms in this
candidate query. Their omission can only enlarge the candidate set.

Request 32,769 candidates. When the query exhausts with at most 32,768 unique
documents and reports no unseen tail, intersect the unchanged original query
with a zero-boost document-identity query over that complete set. Use the
original query for final scoring, ordering, field scores, and totals. A complete
empty set is an honest empty result and does not run the scoped query. An
explicit diagnostic explanation bypasses the candidate stage so its public
Bleve explanation tree remains byte-for-byte equivalent to the established
query.

When the sentinel is returned, the reported total exceeds the returned set, a
shard is incomplete, or candidate collection fails, do not truncate or claim
an exact result. Operational failures remain failures. An over-limit candidate
set uses the established exhaustive query path. The fixed limit is retrieval
policy rather than an operator setting and introduces no environment control.

Admit disk reads at
`max(1, GOMAXPROCS / eight Scorch shards)` per process. Admission waits only
inside the caller's existing context and releases on every completion,
cancellation, and cancellation race. The existing outer search, remote, fuzzy,
and HTTP admissions remain unchanged.

Treat this admission as overload protection. A target that must complete
hundreds of simultaneous searches provisions independently measured read
replicas so the queue on every replica remains within its validated service
time. The warm-copy measurements imply that a 200-request burst needs at least
six evenly loaded four-CPU replicas by work divided by budget; eight provide
warm-copy headroom. This is not a production sizing claim. Production storage,
corpus, query mix, and cold synchronized measurements remain authoritative.

A load-balanced replicated-read runtime is not introduced here. It remains a
new service boundary and requires a separate ADR plus synchronized Docker,
systemd, package, state-replication, health, and rollout work before
implementation.

## Consequences

Eligible ordinary queries perform two bounded searches instead of one. Warm
single-query latency can increase, especially near the candidate ceiling, while
the avoided analyzer-scope scan reduces the cold and constrained-read work that
caused the production failure. The existing final-result cache avoids repeating
either stage for a cached request.

The candidate union is complete or it is not used. No approximate posting
prefix, score heuristic, localized word list, altered BM25 score, stored-format
change, or new dependency is introduced. Queries broader than the fixed limit
retain exact legacy behavior and remain the future persisted block-max case.

Tests prove admitted and refused candidate sets, the 32,769th sentinel contract,
unknown-tail refusal, operational and cancellation failures, cancellation-race
release, honest empty answers, analyzer-specific stopwords, strict and relaxed
multi-term queries, domain narrowing, exclusions, expansions, fuzzy recovery,
and exact identity, order, score, field-score, total, and explicit-explanation
equality against exhaustive scoped search. No setting, environment variable,
listener, service, image, package topology, contract, or wire shape changes.
