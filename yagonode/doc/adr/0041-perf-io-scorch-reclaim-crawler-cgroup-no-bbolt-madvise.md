# 0041. PERF-IO-02: scorch delete-reclaim tuning and crawler cgroup limits; do not add raw bbolt mmap madvise

Date: 2026-07-10

## Status

Accepted

## Context

PERF-IO-02 bundled three I/O ideas from the performance survey: tune the scorch
merge policy, apply an mmap `madvise` access-pattern hint to the bbolt shard
vault, and bound the crawler's headless-Firefox resource use with a cgroup. The
first and third are shippable and useful; the second does not survive contact
with the code.

Two facts, both verified against the real `go.etcd.io/bbolt v1.4.3` and
`golang.org/x/tools` source, kill the madvise idea:

1. **bbolt already advises `MADV_RANDOM`.** On linux `bolt_unix.go` calls
   `unix.Madvise(b, MADV_RANDOM)` unconditionally on every mmap, and it remaps
   (re-applying the advice) on every growth. So "opt the shards into
   `MADV_RANDOM`" — the survey's framing — is a literal no-op against current
   behavior. The only advice that would change anything is *overriding* bbolt's
   baked-in `MADV_RANDOM` toward a readahead-friendly mode for a scan-heavy node,
   and even that reverts to `MADV_RANDOM` on the next ingest-driven remap.

2. **The only way to reach the mapping fails the vet gate.** bbolt exposes the
   mmap base solely as `(*bolt.DB).Info().Data`, a `uintptr`. Turning it into the
   `[]byte` that `unix.Madvise` needs requires `unsafe.Pointer(info.Data)`, which
   the `go vet` `unsafeptr` analyzer flags as "possible misuse of unsafe.Pointer"
   (a custom struct's `uintptr` field is not on its whitelist, unlike
   `reflect.SliceHeader`). `make verify` runs a standalone `go vet ./...` whose
   diagnostics no `//nolint` can suppress, and the same line trips `gosec` G103.
   The codebase today contains no `unsafe`, no platform-gated syscalls, and no
   build-tagged files; it is deliberately CGO-free and portable.

Forcing madvise through would mean weakening the `unsafeptr` vet analyzer
repository-wide (or reflecting into bbolt's unexported fields) to introduce the
first `unsafe` syscall code in the tree — all for a hint that is a no-op in its
obvious form, transient in its useful form, and unsupported by any profiling on
this node. The survey itself cites the CIDR'22 "mmap perils" argument against
relying on such hints.

## Decision

Ship the two parts that fit; do not add raw bbolt mmap madvise.

1. **Scorch delete-reclaim tuning.** Raise the disk index's merge
   `ReclaimDeletesWeight` from the 2.0 default to 2.5 — above default, below the
   ~3.0 the planner warns is too aggressive. The node churns its index
   (re-ingest purges a URL's stale postings; eviction deletes whole documents),
   so biasing merge selection toward the most-deleted segments reclaims that
   tombstoned space sooner, keeping segments smaller and cutting the disk a
   search or merge must read — without raising total merge volume the way
   shrinking the tier width would, which matters for an I/O-oriented change.

2. **Crawler cgroup limits.** Add `MemoryHigh=60%`, `MemoryMax=85%`,
   `TasksMax=4096`, and `CPUWeight=50` to the packaged `yagocrawler.service` to
   bound headless-Firefox memory and task growth while giving the co-located node
   greater relative CPU weight. The
   percentages are relative to physical RAM (box-agnostic); `MemoryHigh` throttles
   without killing; a `MemoryMax` out-of-memory kill stays confined to the crawler
   cgroup. A killed Firefox child is replaced by its session manager; repeated
   launch or render failures cool only that session (BROWSER-04).
   `Restart=on-failure` restarts the service if the crawler process is killed;
   `CPUWeight` below the node's default favors interactive search during a crawl.
   Operators tune the values per host with a systemd drop-in.

3. **No bbolt mmap madvise.** Declined for the two reasons above.

## Consequences

- The quality gate is untouched: no `unsafe`, no CGO, no build-tag split, no
  weakened vet analyzer, no promoted dependency.
- Search and merge over a churning index read less disk as deleted space is
  reclaimed faster; the packaged systemd crawler bounds its own cgroup memory and
  gives the node greater relative CPU weight under contention.
- PERF-PRIO-01 was refined on 2026-07-13 after production indexing showed that
  the lowest-possible `Nice=19` and idle I/O policy starved crawler progress under
  normal node load. The cgroup weighting remains, while the process uses
  `Nice=5`, best-effort I/O priority 6, and `IOWeight=50`; the container uses the
  equivalent `ionice` best-effort/6 plus `nice 5`. The crawler still yields to the
  node without losing useful indexing throughput.
- If profiling ever shows the RWI shard mmaps are readahead-bound on a
  scan-heavy node, revisit with a mechanism that does not fight the vet gate — for
  example an upstream bbolt option, or a narrow one-shot `MADV_WILLNEED` prefault
  through a throwaway `unix.Mmap` (which returns a `[]byte`, so it needs no
  `uintptr` round-trip), labelled a prefault rather than a persistent policy.

## Alternatives considered

- **Opt-in `MADV_RANDOM` bool (the survey's framing).** Rejected: bbolt already
  applies `MADV_RANDOM`, so it would be a no-op.
- **Override to `normal`/`sequential`/`willneed` via `Info().Data`.** Rejected: the
  `unsafe.Pointer(uintptr)` conversion fails `go vet`/`gosec`, and the advice is
  reset to `MADV_RANDOM` on every growth remap regardless.
- **`go vet -unsafeptr=false` to permit the unsafe path.** Rejected: it weakens a
  correctness analyzer for the whole repository to enable one speculative,
  unmeasured feature.
- **Shrinking `MaxSegmentsPerTier` / raising `FloorSegmentSize` for faster search.**
  Rejected for this I/O-focused change: both increase total merge volume, trading
  the I/O this task means to reduce for search latency; revisit only with
  profiling.
