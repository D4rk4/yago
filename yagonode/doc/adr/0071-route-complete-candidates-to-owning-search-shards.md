# 0071. Route complete candidates to owning search shards

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0070](0070-bound-analyzer-scoped-search-with-complete-lexical-candidates.md).

## Context

Release v0.0.49 replaced an exhaustive analyzer-scope scan with a complete
positive lexical candidate set, but a production cold probe still exhausted
the 1.8-second response budget. A repeat completed within the budget. The node
had no restart, cgroup pressure, major fault, or physical input evidence during
the failed probe.

A CPU profile of 200 searches over a read-only copy of the production index
used 40.36 CPU-seconds in 10.39 wall-clock seconds on four processors. About
69 percent of cumulative CPU time was under Scorch and zapx document-number
resolution, principally vellum FST lookup. The zero-boost document-identity
restriction supplied the complete candidate list to the Bleve alias. Every one
of its eight child indexes therefore resolved identities owned by all other
children, and each child supplied that list to every active segment.

Candidate hits already identify the Bleve child that produced them. Searching
each child separately with only its identities reduced the same burst to about
3.9 seconds, but a manual result merge changed the order of equal-score hits.
That approach would replace Bleve's established merge, status, error, scoring,
and pagination behavior and is not acceptable.

The positive candidate query also repeated clauses whose analyzers emitted the
same exact token stream for one term. Bleve MatchQuery creates its term clauses
from that analyzed stream. With the same fields, boosts, fuzziness, prefix, and
unscored union, such clauses address the same postings and add no recall.

## Decision

For each positive candidate term, retain the first analyzer for each exact
token stream. Retain an analyzer when its token stream cannot be obtained.
Retain every Chinese or Japanese dictionary branch independently because its
runtime dictionary availability can change the query. Do not infer a language
or prune by script, spelling, or a semantic word list.

Construct the disk read alias from transparent child wrappers. Before a child
executes `SearchInContext`, its wrapper adds only that child's stable position
to the request context. Every other index operation, child name, mapping, and
result remains the underlying Bleve operation.

When the complete candidate result names every hit's originating child and all
configured child names are nonempty and unique, group the candidate identities
by that exact child name. Intersect the unchanged final query with a private
zero-boost identity query. On each wrapped child, that query supplies only the
identities owned by the child's position. Without a recognized position it
supplies the original complete flattened list, preserving compatibility with a
plain alias.

If a child is absent, a child name is empty or duplicated, a candidate hit is
absent, or its child name is unknown, use Bleve's established alias-wide
document-identity query. No uncertain ownership may narrow the result.

Execute the final conjunction through the same single Bleve alias. Bleve
continues to own child concurrency, scoring, result merge, equal-score order,
pagination, status, errors, cancellation, and field-score behavior. The private
identity query exposes `_id` to Bleve's field traversal. Explicit diagnostic
explanations retain the exhaustive path selected by ADR-0070.

Treat the four-CPU production-copy burst as capacity evidence, not a promise.
The exact native-alias implementation completed two 200-request rounds in
4.330 and 3.808 seconds, with p99 of 4.326 and 3.781 seconds. The established
alias-wide identity query completed comparable rounds in 10.393 and 11.279
seconds, with p99 of 10.183 and 11.161 seconds. The nine measured query shapes
retained every candidate identity and every final identity, order, score,
total, and status.

The slower corrected round implies `4.330 / 0.600 = 7.22` equally loaded
four-CPU capacity units for 200 synchronized requests inside the ordinary
600-millisecond exact-stage budget. Eight active read replicas are therefore
the initial architecture target, and an N+1 deployment starts at nine. This is
only capacity arithmetic. Production acceptance still requires the complete
cold HTTP path, the actual corpus and query mix, independent local index state,
balanced routing, and a one-replica-loss test.

This decision does not introduce the replicated-read runtime. That remains a
new service boundary requiring its own ADR and synchronized state replication,
health, routing, Docker, systemd, Debian package, rollout, and rollback design.

## Consequences

The final rerank resolves each known candidate identity in one owning child
instead of all eight children. The grouping is bounded by the existing 32,768
candidate ceiling. The alias adds one small request-context value per child
search and retains the flattened identities for the conservative fallback.

No manual result merge, approximate pruning, score change, stored-format
change, dependency, setting, environment variable, listener, service, image,
package topology, contract, or wire shape is introduced.

Tests prove exact analyzer-stream preservation, distinct-stream and dictionary
retention, unavailable-analyzer fallback, exact child ownership, every
ambiguous-ownership refusal, plain-alias fallback, child selection, cancellation
and operation failure propagation, native-alias status behavior, and exact
identity, order, score, field-score, total, and explanation compatibility with
the established retrieval path. Package statement coverage and the race suite
remain complete.
