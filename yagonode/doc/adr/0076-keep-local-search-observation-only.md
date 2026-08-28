# 0076. Keep local search observation-only

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0066](0066-read-visible-search-evidence-as-a-document-set.md)
and [ADR-0073](0073-limit-native-search-admission-to-page-execution.md).

## Context

Production release v0.0.54 returned eight new local-only queries as incomplete
empty answers after roughly two seconds each. During those reads the Bleve
document total fell from 557,517 to 557,156. Candidate and evidence search
filtered a missing stored document, then synchronously called the ordinary
Bleve deletion path for its identity. One request could discover many
historical or crash-drifted identities and turn a latency-sensitive read into a
sequence of index mutations.

Bleve implements one `Delete` call as a single-item mutation batch. Scorch
persists mutation batches and plans segment merges asynchronously, so a search
that waits for deletion can inherit indexing and persistence work. See Bleve's
[index implementation](https://github.com/blevesearch/bleve/blob/v2.6.1/index_impl.go)
and
[Scorch persister documentation](https://github.com/blevesearch/bleve/blob/v2.6.1/docs/persister.md).
The component-tail result in
[*The Tail at Scale*](https://research.google/pubs/the-tail-at-scale/)
supports removing unrelated work from a fanned-out read path. The read/write
latency analysis in [arXiv:2108.13949](https://arxiv.org/abs/2108.13949)
supports treating reconciliation writes as a separate capacity concern.

Current document lifecycle operations already own ordinary index removal.
Admin deletion, quota eviction, redirect cleanup, and crawler tombstones remove
the document and its Bleve identity through the bounded lineage owner. The
operator-triggered full-index rebuild reconstructs derived Bleve state from the
authoritative document store. Search-time deletion duplicates those ownership
boundaries and makes query traffic determine maintenance load.

## Decision

Keep every local search phase observation-only. Candidate presence, stored
projection, filtered or faceted scans, and visible evidence hydration may
classify a Bleve identity as missing from the document store. They filter that
identity from visible results, adjust the bounded result estimate, and may open
the established continuation page when an unseen tail exists. They do not
index, delete, or enqueue a Bleve mutation.

Keep current document removal at the explicit lineage boundary. Reconcile
historical or crash-created full-text drift through the existing explicit
full-index rebuild. Any future incremental reconciliation worker must own its
queue, budgets, cancellation, and observability independently of request
execution and requires a separate accepted decision before it can mutate the
index.

Preserve missing-document authority. Search never trusts a Bleve projection
after the document store says its identity is absent, and it never exposes a
missing document merely to avoid a write. An honest empty answer remains an
empty answer rather than an infrastructure failure.

The horizontal read-tier requirement remains unchanged. Replicas must own
independent immutable generations and independent processor and I/O capacity;
removing request-time writes does not make one four-processor node a
hundreds-request capacity guarantee.

## Consequences

A missing stored document can remain in Bleve until an explicit lifecycle
mutation or full rebuild. It consumes bounded candidate work but cannot turn a
read into Scorch persistence or merge work. Request latency and index mutation
capacity are no longer coupled through search traffic.

Permanent tests hold the Bleve mutation lock while searching a missing
candidate and require the read to complete. Candidate and both evidence paths
must leave the index document total and update timestamp unchanged for missing
and already-correct input.

A disposable clone of the production Bleve tree was searched with a presence
source that rejected every candidate. The same eight production terms
completed in 11.220 to 54.447 milliseconds, while the index remained exactly
559,321 documents with an unchanged update timestamp. The diagnostic source
was removed after the isolated check; candidate code never opened production
data.

The change adds no dependency, stored format, setting, environment variable,
listener, service, image, package topology, contract, or wire shape.
