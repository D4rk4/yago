# 0075. Stop disk term-reader work at the request deadline

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0074](0074-stop-fuzzy-dictionary-work-at-request-deadline.md)
and [ADR-0073](0073-limit-native-search-admission-to-page-execution.md).

## Context

Release v0.0.53 stopped fuzzy vocabulary enumeration after cancellation, but
production acceptance still found an exact-search overrun. Seven of eight new
local-only terms completed with results in 0.328 to 1.214 seconds.
`proteomics` returned zero rows with a local-search deadline failure after
1.955 seconds, then the node consumed about 3.99 CPU-seconds during the next
three seconds. A fresh `metabolomics` request limited to one result reproduced
the failure and late CPU. Both immediate repeats were fast and result-bearing.

Bleve v2.6.1 stores the caller context on each Scorch term reader but does not
inspect it while opening per-segment dictionaries, posting lists, and
iterators. Its `Next` and `Advance` paths likewise enumerate postings without
inspecting that context. See the upstream
[term-reader construction](https://github.com/blevesearch/bleve/blob/v2.6.1/index/scorch/snapshot_index.go)
and
[term-reader enumeration](https://github.com/blevesearch/bleve/blob/v2.6.1/index/scorch/snapshot_index_tfr.go).
A request can therefore lose its response objective while exact query
construction or enumeration continues to consume native-page capacity.

Response-time admission research supports stopping work after its objective is
lost. It does not make one process capable of an arbitrary burst. Independently
provisioned read capacity remains necessary for the operator's requirement that
hundreds of simultaneous requests meet one latency budget.

## Decision

Wrap every disk-backed requested-hit and complete-query page with one
deadline-aware query boundary. Preserve the original query's validation.
In-memory search remains unchanged.

Within that boundary, inspect the request context immediately before and after
opening each term reader and immediately before and after each `Next` and
`Advance` call. A canceled reader performs no later operation. A reader
returned after cancellation is closed before the cancellation cause is
returned. Existing open, read, advance, and close failures remain observable;
honest end-of-postings remains an ordinary empty result.

Preserve Scorch term-reader optimization. The wrapper exposes optimization only
when the underlying reader supports it, carries the deadline through the shared
optimization context, and wraps any optimized term reader returned by
`Finish`. It also preserves the optional fuzzy-automaton and BM25 reader
capabilities only when the underlying index reader provides them. A canceled
reader reports zero cardinality without invoking the underlying count path, so
query construction reaches its cancellation error without another posting
walk.

Keep cancellation synchronous. Do not add a query goroutine, detached cleanup
path, timeout, cache, or admission channel. The bound is one already executing
underlying open, read, advance, optimization, or close step. The wrapper cannot
interrupt that step inside Bleve.

Treat the hundreds-request target as a horizontal read-tier invariant. A future
read tier must give each replica independent processors, I/O capacity, an
independently owned immutable search generation, and a query-ready document
projection. It must route only to ready replicas within the freshness bound,
include admission and queue time in its latency measurement, and retain the
target with one replica unavailable. Processes must never share one live
mutable Scorch directory. That runtime boundary still requires its own accepted
and implemented topology ADR plus synchronized Docker, systemd, package,
rollout, and rollback changes.

## Consequences

Deterministic tests prove both directions for query validation, term-reader
open, `Next`, `Advance`, count, close, optimization, optimized-reader
completion, fuzzy and BM25 capabilities, and both disk page paths. Live
operations retain their values and failures; pre-canceled operations do no
underlying work; cancellation during one operation returns after that operation
and refuses the next.

On the disposable production-index copy, four production-shaped exact queries
returned identical totals, ordered hit identities, BM25 scores, and Bleve costs
with and without the boundary. Warm bounded pages completed in 2.12 to 6.54
milliseconds. The copy was isolated from production and its writable-open
metadata was disposable.

The change adds no dependency, stored format, setting, environment variable,
listener, runtime service, image, package topology, contract, or wire shape.
It bounds abandoned work in the current process; it does not claim that the
current four-processor node satisfies the final hundreds-request target.
