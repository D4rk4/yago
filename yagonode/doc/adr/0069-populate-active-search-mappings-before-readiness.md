# 0069. Populate active search mappings before readiness

Date: 2026-08-27

## Status

Accepted

Amends [ADR-0068](0068-load-active-search-segments-before-readiness.md).

## Context

ADR-0068 made startup read every active persisted Scorch root before readiness.
Release v0.0.47 performed about 2.30 GB of that sequential input on production,
but five new common queries still exhausted the 1.8-second local-search budget.
Their later repeats completed in 51 to 102 milliseconds.

The new query `economics` incurred 16,263 minor faults, no major fault or
physical input, and about 3.32 CPU-seconds before its cold request timed out.
Its repeat incurred 490 minor faults, used about 0.12 CPU-seconds, and returned
ten rows in 58.9 milliseconds. The active bytes were already in the Linux page
cache, but the page tables for Scorch's previously opened zapx mappings were
still cold. Reading a file through a separate descriptor does not populate page
table entries for that mapping.

The pinned zapx segment exposes its live file-backed mapping as `Data()`.
Scorch snapshots retain segment lifetime while a query or startup operation
uses it. A disposable 100,000-document, eight-shard benchmark reopened fresh
mappings after loading the same files through separate descriptors. The first
broad query incurred 46 minor faults and took 16.34 milliseconds. Touching the
live mappings first reduced that query to zero measured minor faults and 15.30
milliseconds. The fixture confirms the page-table mechanism; it does not claim
production throughput or a general capacity bound.

The existing production-sized burst tests remain authoritative for capacity.
Corpus and CPU allocation materially change scored-search cost, and a four-CPU
replica cannot honestly guarantee hundreds of simultaneous broad queries inside
the interactive budget.

## Decision

Keep ADR-0068's sequential file read. While the same referenced Scorch snapshot
is still held, select the corresponding live mapping for every nonempty
persisted segment path. Require the mapping to be present and require its byte
length to equal the number of bytes read from the active file. A disagreement
refuses index construction and readiness.

After the file read, access one byte at every operating-system page boundary in
the live mapping. Check startup cancellation before population and at every
page boundary. Fold the accessed bytes into the bounded completion record so
the reads remain observable. Keep one shard snapshot and one root file active
at a time, and finish each mapping before advancing to the next segment.

A segment without a persisted path remains an in-memory segment and is skipped.
A nonempty persisted path without a mapping, a nil mapping, an invalid page
size, cancellation, or a file-to-mapping size mismatch refuses startup. A
non-nil empty mapping and its empty file remain valid unchanged input.

The population step does not write the segment, copy its complete contents,
lock pages in memory, or change the writer generation. File-backed pages remain
reclaimable. The existing lexical warm-up still runs afterward.

Treat per-process admission as overload protection only. A deployment that
must serve hundreds of simultaneous requests within a latency budget requires
enough independently measured read replicas, or a separately accepted bounded
retrieval backend, to keep every replica within its CPU and cache envelope. A
new replicated-read runtime remains a separate service-boundary decision with
the required Docker, systemd, and package topology work.

## Consequences

Startup performs one additional page-granular pass over each active mapping
after its sequential file read. It pays the mapping fault cost before readiness
instead of charging an arbitrary first user. Page-table memory and the active
file-backed working set contribute to process and cgroup residency, and the
operating system may reclaim either later; this is not page pinning.

Tests prove page-boundary access, unchanged non-boundary bytes, empty-input
acceptance, pre-operation and mid-operation cancellation, active persisted
segment selection, in-memory and empty-path skipping, unavailable and nil
mapping refusal, file-to-mapping size refusal, report aggregation, and real
Scorch mapping/file size equality. The disposable benchmark records both
first-query latency and minor faults. No stored format, dependency, setting,
environment variable, listener, service, image, package topology, contract, or
wire shape changes.
