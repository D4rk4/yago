# 0072. Rerank lexical candidates within one search snapshot

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0070](0070-bound-analyzer-scoped-search-with-complete-lexical-candidates.md)
and [ADR-0071](0071-route-complete-candidates-to-owning-search-shards.md).

Amended by [ADR-0078](0078-use-requirement-aware-lexical-candidates.md).

## Context

Release v0.0.50 routed complete candidate identities to their owning Bleve
children, but production cold-search acceptance still exhausted the
1.8-second request budget. A new `seismology` request returned an incomplete
answer after 1.821 seconds. The node used 2.36 CPU-seconds before the response
and continued to 4.84 CPU-seconds two seconds later, with no physical input.
Its candidate set contained 113 documents. Immediate repeats completed within
the budget.

The v0.0.50 path used two Bleve searches. The first materialized external
document identities and child names. The second opened a new reader snapshot
and resolved those external identities back to segment-local document numbers
before the unchanged scoped query could score them. Ownership removed
cross-shard lookups but did not remove this external-to-internal round trip.

Bleve v2.6.1 acquires one `IndexReader` before constructing a query searcher,
uses that reader through collection, and closes it afterward. Scorch returns a
referenced current snapshot for that reader. Segment-local identities are
therefore safe only while the searcher uses that same reader. Carrying them to
a second request would make them invalid after a concurrent snapshot change.
The relevant upstream boundaries are
[`index_impl.go`](https://github.com/blevesearch/bleve/blob/v2.6.1/index_impl.go)
and
[`scorch.go`](https://github.com/blevesearch/bleve/blob/v2.6.1/index/scorch/scorch.go).

A disposable implementation enumerated lexical candidates and reranked them
inside the same child reader. Across thirteen production-copy terms it retained
the exact final identities, order, scores, totals, and status. After the normal
startup cache and mapping preparation, a freshly reopened `seismology` search
improved from 54.03 milliseconds to 6.43 milliseconds with the same 51 returned
hits and total of 113.

On four processors, 200 mixed same-snapshot requests completed in 3.331 seconds
with one admitted request and 2.757 to 2.957 seconds with wider admissions.
ADR-0073 later replaced that whole-search single-width admission after
production proved it created a convoy outside native Bleve work. This historical
measurement did not make one replica a 200-request tier.

## Decision

An ordinary analyzer-scoped disk request executes one native Bleve alias search.
Its final query is the conjunction of the unchanged analyzer-scoped query and a
private lexical-candidate snapshot query. Bleve remains responsible for opening
each child reader, constructing both searchers against that reader, scoring,
child concurrency, result merge, equal-score order, pagination, status, errors,
and cancellation.

Inside each child reader, enumerate the unscored disjunction of the distinct
positive lexical requirements. Retain exact analyzer-token coalescing and the
conservative unavailable-analyzer and CJK dictionary behavior established by
ADR-0071. Request at most 4,097 matches from that reader. Copy each internal
identity before returning its match to Bleve's pool.

When the child query exhausts at 4,096 documents or fewer, sort those internal
identities and expose them through a zero-score searcher bound to the same
reader. An empty complete set becomes MatchNone. The 4,097th match becomes a
zero-score MatchAll filter, so the unchanged scoped query remains exhaustive for
that child. Cancellation, missing identities, searcher construction, iteration,
or close failures remain operational failures and cannot become a partial
success.

The per-child limit is conservative. Eight accepted children can admit at most
the established 32,768 candidates. A skewed child may fall back to exhaustive
scoring even when the alias-wide total would have fit; it may never truncate the
child to fit the global limit. Internal identities never leave their
`IndexReader`, enter a cache, or cross a request boundary.

Explicit diagnostic explanations continue to use the established exhaustive
query so their public explanation tree remains unchanged. Field-score extraction
continues through the exact same-snapshot path and must remain equal to the
exhaustive result.

ADR-0073 replaces the whole-search single-request admission with a
processor-sized native-page admission. Its new production-copy measurement is
the current local candidate-capacity evidence. Representative cold HTTP service
time, balanced routing, replica freshness, and loss of one replica must still
pass before claiming a replicated target.

The replicated-read runtime remains a separate service boundary. It requires a
separate accepted ADR and synchronized state replication, routing, health,
Docker, systemd, Debian package, rollout, and rollback design before
implementation.

## Consequences

Eligible requests no longer materialize candidate external identities or open a
second reader to resolve them. The original scoped query remains authoritative,
and candidate overflow enlarges work instead of narrowing recall. Broad or
skewed terms retain exhaustive behavior and remain the future persisted
block-maximum case.

The change removes the transparent child wrappers, request-context shard
positions, external-identity grouping, and alias-wide compatibility branch from
the active path. It adds no manual merge, approximate pruning, stored-format
change, dependency, setting, environment variable, listener, service, image,
package topology, contract, or wire change.

Tests prove admitted, empty, and overflow behavior; copied and sorted internal
identities; same-reader construction; cancellation before and during
collection; query, iteration, missing-identity, reader, count, and close
failures; zero-score iteration and advance; exhaustive overflow; native alias
status and error propagation; explicit-explanation bypass; honest empty answers;
and exact identity, order, score, field-score, and total equality with exhaustive
retrieval.
