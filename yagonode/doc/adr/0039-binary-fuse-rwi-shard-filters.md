# 0039. Per-shard binary-fuse term filters to skip missing RWI shards

Date: 2026-07-10

## Status

Accepted

## Context

A reverse-word-index lookup fans out across every shard. A posting is keyed
`wordHash ++ urlHash` and routed by a hash of the whole key, so one word's
postings scatter across all shards (the URL half varies). Finding a word is
therefore a prefix scan that opens a read transaction and seeks a cursor on
*every* shard, and most of those seeks miss — the word lives in only a few
shards. The waste is worst exactly where it hurts: a rare term is present in one
or two shards yet is sought on all of them, and a multi-term AND query gates on
its rarest term, so the whole query pays the fan-out of the term that least
needs it. As the pool grows with the corpus (ADR-0037), the number of wasted
seeks per lookup grows with it.

What is missing is a cheap way to answer, per shard, "could this shard hold this
term?" before opening the transaction and seeking. That is an approximate
set-membership test over each shard's term-key prefixes. The only failure it may
make is a false *positive* — claiming a term might be present when it is not —
which costs a single wasted seek. A false *negative* would skip a shard that
holds the term and silently drop results, so the structure must never report
absent for a present key.

Bloom filters are the classic answer. Binary fuse filters (Graf & Lemire, 2022,
arXiv:2201.01174) are the modern one: immutable once built, ~9 bits per key for a
~0.4% false-positive rate, smaller and faster to query than a Bloom filter, and —
critically — with no false negatives. Their one cost is immutability: a fuse
filter is constructed from a fixed key set and cannot be updated in place.

## Decision

Give each shard a binary-fuse membership filter over its term-key prefixes and
skip, on a fan-out read, the shards whose filter proves the term absent. Adopt an
existing fuse-filter library rather than hand-roll it. One combined ADR because
the dependency and the feature are inseparable.

### A. Dependency: `github.com/FastFilter/xorfilter` v0.5.1

**Need.** Constructing a binary fuse filter correctly is the hard part: the build
is a 3-wise hypergraph peeling with reseed-and-retry on failure. That retry
exhaustion is reached only with a probability "lower than a cosmic-ray bit flip",
so it is effectively unreachable — and therefore impossible to exercise honestly
under this repo's 100%-coverage gate — while a subtle bug in the peel/xor math
would silently drop search results. This is exactly the code to take from the
algorithm's authors rather than reimplement.

**Choice.** `xorfilter` is by Graf and Lemire (the paper's authors), single
purpose, with **zero transitive dependencies**. It exposes generic
`NewBinaryFuse[uint8]` (the ~0.4% FPR width), `Contains`, and construction that
deduplicates keys and errors only on an empty set. It is pinned at **v0.5.1**.

**License.** `xorfilter` is Apache-2.0; this project is AGPL-3.0. Apache-2.0 is
one-way compatible into AGPL-3.0, so bundling it is clean.

**Maintenance and security.** A small, stable, well-cited library with no
transitive surface; it is wrapped in an internal `wordFilter` type so no
third-party type appears on any exported boundary, and the fuse constructor sits
behind a package seam so the storage engine can be tested without it. Trivy and
Semgrep scan it with the rest of the tree.

### B. Static filter plus a mutable side-set

Each shard carries an immutable fuse filter built from the term-key prefixes
present when it was built, plus a small mutable side-set of the keys written
since. Membership is `static.Contains(key) OR key in side-set`. The static filter
is (re)built:

- **at open**, from the shard's persisted keys;
- **at compaction**, folding the accumulated side-set into a fresh static filter
  and resetting the side-set, which bounds the side-set's growth;
- **appended at split**, so a newly split-in shard gets its own filter and the
  one-filter-per-shard invariant holds.

Every write records its term key in the owning shard's side-set, so a read sees a
just-written term before the next rebuild.

### C. Safe by construction

The skip guard engages only for the configured bucket and a full-width term
prefix; every other scan (a different bucket, a partial or empty prefix) is never
skipped. Membership is deliberately conservative: a not-yet-built filter, a
build that failed, or any hit all answer "maybe" and never skip; only a built,
non-degraded filter that misses both the static set and the side-set answers
"absent", and that is the sole answer that skips a shard. A construction failure
degrades the filter to matching everything, so a filter glitch can slow a read
but never hide a result.

Concurrency rests on the engine's existing gate discipline: rebuilds run under
the exclusive gate (or pre-serving at open), while a write's side-set add runs
under the shared gate for the whole update, so exclusive and shared never
overlap and a committed key is never absent from *both* the static filter and the
side-set. Deletes need no hook — they leave stale *positives* (a harmless wasted
seek), reclaimed at the next compaction rebuild.

### D. Injected, opt-in, engine stays RWI-agnostic

The storage engine must not depend on the RWI package (an architecture
boundary), so the assembly layer injects the filtered bucket name and the
term-prefix width through a `WithWordFilter` open option. Without it the filters
stay off and reads never skip, so the optimization is opt-in at the wiring layer
and existing callers are unchanged. The baseline builds filters eagerly at open
(deterministic, simplest to reason about); a lazy background build and on-disk
persistence of the fuse are recorded as later optimizations — persistence is only
safe if paired with side-set persistence, since a crash between the two would
drop results.

## Consequences

- A rare-term or multi-term read seeks only the shards that might hold the term,
  skipping the rest; the saving grows with the shard count, which is where the
  fan-out cost was growing.
- Memory cost is about nine bits per distinct term key per shard, plus the
  side-set between rebuilds.
- Open pays one term-key scan per shard to build the filters — comparable to the
  freelist rebuild bbolt already does on open (IO-AGG-03); noted for a large
  index, with lazy build available as a follow-up.
- One new third-party dependency (`xorfilter`), justified above; no wire-format,
  peer-protocol, or on-disk-layout change — the filters are an in-memory read
  accelerator rebuilt from the shards.
- Correctness is one-directional: the filters can only ever cause a *wasted*
  seek, never a *skipped* result, so a filter bug degrades speed, not answers.

## Alternatives considered

- **Bloom filter.** Same no-false-negative guarantee, but larger and slower to
  query than a binary fuse at the same false-positive rate. Rejected on size and
  speed; the fuse is strictly better for a static, rebuilt-in-bulk key set.
- **Cuckoo or counting filter with in-place deletes.** Would avoid the
  side-set/rebuild dance by supporting mutation, but costs more bits, adds work to
  the delete hot path, and complicates the concurrency story — for deletes that
  are rare and already stale-positive-safe. Rejected.
- **Persist the fuse on disk.** Avoids the open-time scan, but is unsafe unless
  the in-memory side-set is persisted too; a crash between a side-set add and its
  persistence would drop results. Deferred behind the safe rebuild-at-open
  baseline.
- **Hand-roll the fuse construction.** Avoids the dependency but re-creates a
  peeling algorithm whose failure path cannot be honestly covered and whose bugs
  drop results silently. Rejected in favor of the authors' library.
- **A per-shard exact key index.** Would answer membership exactly, but that is
  precisely the bbolt bucket the scan is already seeking — it defeats the purpose.
  Rejected.
