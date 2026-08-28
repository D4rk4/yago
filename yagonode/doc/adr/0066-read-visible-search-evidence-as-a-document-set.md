# 0066. Read visible search evidence as a document set

Date: 2026-08-27

## Status

Accepted

Amended by [ADR-0076](0076-keep-local-search-observation-only.md).

## Context

Disk search validates candidate presence before ranking, then hydrates stored
documents for the visible evidence pass that produces snippets, query spans,
and phrase or proximity evidence. ADR-0065 made candidate presence
shard-parallel. Production v0.0.43 still exhausted the 1.8-second interactive
budget on cold common queries: each first request returned an incomplete empty
answer, while the immediate repeat returned ten rows in less than 270
milliseconds.

The remaining evidence pass loaded as many as ten documents through ten
separate URL-lock acquisitions and ten vault snapshots. Each load first read a
location row and then its ordered document row or legacy URL row. These were
independent point reads over mmap-backed bbolt B+trees and independent physical
shard files.

A disposable synthetic vault stored 160,000 documents with incompressible 8
KiB bodies over eight physical shards, occupying 1,778,184,192 bytes. Ten
selected identities covered every shard in both the location and document-row
phases. Before each measurement the vault was closed, every shard file received
`POSIX_FADV_DONTNEED`, and the vault was reopened. Sequential full-document
loads took 13.823 and 10.972 milliseconds. One document-set load took 4.178 and
5.405 milliseconds, 3.31 and 2.03 times faster. Every returned identity and
body was checked. The test and its data were removed afterward.

The official [bbolt transaction documentation](https://pkg.go.dev/go.etcd.io/bbolt#Tx)
permits independent read-only transactions while excluding concurrent use of
one transaction or its buckets. The B+tree result in
[arXiv:1201.0227](https://arxiv.org/abs/1201.0227) supports overlapping
independent flash point reads; the disposable project-specific measurement is
the applicable evidence for this decision.

## Decision

Add an optional `DocumentSetDirectory` beside `DocumentDirectory`. It returns
position-aligned documents and presence decisions for an ordered URL set.
Backends without this capability retain the established one-document path.

The document vault acquires the complete URL read boundary once and opens one
logical vault view. It reads location values as one set, derives exact ordered
keys or legacy URL keys, then reads each document set through the shard-owned
boundary accepted in ADR-0065. Each touched physical shard owns one read-only
bbolt transaction and one reader. No transaction or bucket is shared between
goroutines.

An ordered or legacy row is authoritative only when its decoded
`NormalizedURL` exactly matches the requested identity. Missing rows remain
honest absences. A corrupt document row retains the established compatibility
behavior and is treated as absent; the fallback runs inside the same snapshot.
Operational storage errors, malformed locations, invalid ordered identities,
and context cancellation remain failures.

Disk search uses the set capability only for the existing visible evidence
limit of ten results. It does not hydrate the Bleve overfetch tail. Orphaned
index entries are filtered without mutating Bleve on the request path. Deadline
behavior remains unchanged: strict candidates not yet enriched retain their
bounded projection, while relaxed candidates without stored passage evidence
are not admitted.

## Consequences

Cold visible-document reads can overlap independent shard I/O without changing
result identity, ordering, ranking rules, or snippet semantics. Warm reads add
bounded coordination across at most the touched physical shards. A corrupt row
may select the slower compatibility path, but it cannot invalidate another
correct row or escape the logical snapshot.

Stored formats, vault routing, Bleve mappings, crawler contracts, settings,
environment variables, listeners, service topology, package topology, and
public wire shapes do not change. Tests cover ordered, legacy, missing,
duplicate, corrupt, mismatched, cancelled, malformed, closed-vault, strict,
relaxed, and unsupported-directory paths. The synthetic result is release-gate
evidence rather than a permanent benchmark or a latency guarantee for every
host.
