# 0065. Read exact document presence by physical shard

Date: 2026-08-27

## Status

Accepted

## Context

Disk search retains a bounded Bleve candidate projection, then verifies that
each projected URL still has an authoritative document row. v0.0.41 replaced
one vault transaction per candidate with one snapshot, and v0.0.42 stopped
eagerly validating Bleve's fourfold overfetch tail. Production cold queries
still exhausted the 1.8-second interactive budget because the lexical reranker
requests 100 candidates and the snapshot read their location and document keys
sequentially. Warm repeats succeeded after the mmap pages became resident.

bbolt permits independent read-only transactions, but one transaction and its
buckets are not safe for concurrent use. Its point reads traverse mmap-backed
B+tree pages, so cold keys routed across independent shard files can issue
independent storage work. One logical snapshot must still preserve URL
coordination, result order, and exact-row validation.

A disposable 160,000-document test used eight physical shards, incompressible
4 KiB rows, and 100 two-phase identities. Before every measured round it closed
the vault, advised the operating system to discard every shard file from cache,
and reopened the vault. Sequential cold reads took 91.248 and 95.864
milliseconds. Physical-shard reads took 27.610 and 33.602 milliseconds, 3.30
and 2.85 times faster. The test data never touched the production directory.

The official [bbolt transaction documentation](https://pkg.go.dev/go.etcd.io/bbolt#Tx)
defines the ownership constraint. The B+tree parallelism result in
[arXiv:1201.0227](https://arxiv.org/abs/1201.0227) supports the storage rationale;
the project-specific synthetic test supplies the applicable evidence.

## Decision

Keep stored-candidate presence inside one URL read-lock scope and one logical
vault view. Add an optional ordered set-read capability below typed keyspaces.
Backends without that capability continue the existing per-key reads and return
the same position-aligned values and presence decisions.

The sharded vault groups supplied keys by their current physical routing. It
opens one read-only bbolt transaction for every touched shard before reading,
then assigns exactly one goroutine to each touched transaction and bucket. No
transaction or bucket crosses goroutine ownership. Results are written to their
original positions, and the lowest routed-shard failure is returned after every
reader joins. Context cancellation stops later reads. Value reads retain the
stored checksum and compression decode boundary; non-corruption storage errors
remain latched on the enclosing transaction.

Document presence remains a two-phase exact check. The first phase decodes the
location records. The second phase checks the derived ordered-document keys, or
the legacy URL keys when no location exists. A location record is routing
evidence only and never substitutes for the corresponding stored document row.
Missing rows remain honest absences, not storage failures.

Parallel work is bounded by both the supplied key set and the number of touched
physical shards. The search caller continues to bound that set to one requested
candidate window and opens another window only when result continuation needs
it. No background prefetch, persistent cache, setting, or runtime dependency is
introduced.

## Consequences

Cold point reads can use independent shard files concurrently without weakening
snapshot consistency or document authority. Warm reads retain small goroutine
coordination overhead, bounded to touched shards. A shard with several keys
reads them serially through its single owner, matching bbolt's transaction
safety contract.

Stored formats, vault routing, Bleve mappings, ranking, result order, public
wire shapes, crawler contracts, and deployment topology do not change. The
compatibility fallback keeps other vault engines correct, but only the sharded
production engine gains physical parallelism.

Tests cover present and absent keys, duplicates and input order, ordered and
legacy document rows, fallback behavior, malformed result shapes, corrupt
values, cancellation, unavailable shards, and actual overlap between distinct
shard readers. The synthetic measurement is a release-gate observation, not a
new benchmark dependency or a guarantee about every host's storage latency.
