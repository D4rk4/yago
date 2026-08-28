# 0074. Stop fuzzy dictionary work at the request deadline

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0073](0073-limit-native-search-admission-to-page-execution.md).

## Context

Release v0.0.52 removed the whole-search native admission convoy, but its
production acceptance still failed. Seven of eight new sequential local terms
returned incomplete after 1.801 to 1.817 seconds. A new `endocrinology` request
used about 0.24 CPU-seconds before its 1.825-second response, incurred no major
fault or physical input, and performed most of its additional work after the
response. Its immediate repeat completed in 83.6 milliseconds.

A new `paleoneurology` request isolated the recovery path. The exact search
completed as an honest miss, fuzzy recovery returned its bounded deadline
failure after 180.75 milliseconds, and node threads continued executing after
the response. This late work retained native page admission and competed with
later exact searches even though it could no longer contribute to its caller.

Bleve v2.6.1 builds a fuzzy searcher by enumerating matching entries from every
applicable field dictionary. Its `findFuzzyCandidateTerms` loop repeatedly calls
`FieldDict.Next` without inspecting the supplied context. The current upstream
main branch has the same boundary. `SearchInContext` therefore cannot stop this
query-construction phase at its caller's deadline. The relevant upstream source
is
[`search_fuzzy.go` at v2.6.1](https://github.com/blevesearch/bleve/blob/v2.6.1/search/searcher/search_fuzzy.go)
and
[`search_fuzzy.go` on main](https://github.com/blevesearch/bleve/blob/master/search/searcher/search_fuzzy.go).

The defect is independent of the corrupt posting-frequency OOM recorded in
Bleve issue 2393. Project latency, admission, and operator storage limits do not
belong in that issue.

The response-time admission work in
[Bouncer](https://arxiv.org/abs/2312.15123) supports rejecting or stopping work
that cannot finish within its class objective instead of letting it consume
capacity after the objective is lost. Tail-tolerant distributed-search work
still supports independent replicas for provisioned burst capacity; local
cancellation is necessary but does not replace that topology.

## Decision

Wrap every disk-backed fuzzy Bleve query with a deadline-aware reader boundary.
The wrapper retains the original reader's base contract. It preserves the
optional fuzzy-automaton and BM25 reader capabilities only when the original
reader provides them; it never invents an unsupported capability.

Every field dictionary used to construct the fuzzy query checks the request
context immediately before and immediately after each underlying `Next` call.
A canceled context returns its cause before another dictionary entry can be
enumerated. The ordinary Bleve close path still closes the underlying
dictionary. Existing dictionary errors, cardinality, byte accounting, fuzzy
automata, BM25 field cardinality, scoring, result identity, and ordering remain
unchanged.

Apply the boundary to ordinary requested-hit disk queries and complete filtered
or faceted disk queries. Analyzer-scoped lexical candidate enumeration in the
same query receives the same wrapped reader. Exact, morphological, relaxed, and
in-memory searches retain their existing queries and reader identities.

Do not add another goroutine, detached cleanup path, timeout, or admission
channel. Cancellation stays synchronous: the native Bleve page returns only
after its active dictionary step and normal close complete, so its page token is
released with no hidden fuzzy work left behind.

The bound is one underlying dictionary step plus close latency. The wrapper
cannot interrupt a single `Next` call while that call is executing. A future
upstream context-aware dictionary API could replace this project boundary after
equivalent cancellation, scoring, and production-copy evidence.

Hundreds of simultaneous successful requests remain a provisioned horizontal
read-capacity requirement. Independently owned read replicas, queue-inclusive
load measurement, freshness routing, and one-replica-loss capacity still need a
separate topology ADR before implementation.

## Consequences

On the disposable production-index copy, the exact production-shaped fuzzy page
with a 125-millisecond context returned after 356.6 milliseconds before this
boundary. The same query returned after 143.5 milliseconds with the boundary,
carrying the same deadline failure. The boundary introduces no detached query
goroutine. The
remaining overrun is bounded close and in-flight dictionary-step work rather
than continued enumeration of the complete dictionary.

Tests prove both directions. A live context admits dictionary entries and
preserves end-of-dictionary, error, close, cardinality, byte-accounting, fuzzy,
and BM25 behavior. Cancellation before searcher construction opens no reader.
Cancellation before a dictionary read invokes no underlying read. Cancellation
during one dictionary step returns the context cause after that step, performs
no second step, and closes the dictionary. Reader capability tests prove that
supported optional interfaces remain available and unsupported ones remain
absent. Wiring tests prove both disk page paths apply the fuzzy-only boundary;
exact queries remain unchanged. Existing scoped fuzzy-result tests preserve
identity, BM25 order, and score behavior.

The change adds no dependency, stored format, setting, environment variable,
listener, runtime service, image, package topology, contract, or wire shape.
