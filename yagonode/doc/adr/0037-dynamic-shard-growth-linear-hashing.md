# 0037. Dynamic shard growth by linear hashing; live soft admission quota

Date: 2026-07-09

## Status

Accepted

## Context

An operator set the storage quota to 768 GB in the admin console
(Configuration → Storage) and the Overview kept showing 1 GB. The setting is
inert, and tracing it exposes a deeper design defect in the sharded vault
(ADR-0025).

**1. The vault opens before its own settings can be read.** Runtime setting
overrides are persisted *inside* the vault (the settingsstore is a vault
bucket). So the vault must be opened — with the environment quota — before the
`storage.quota` override can be read back (`internal/yagonode/main.go` opens the
vault at boot, then `loadRuntimeSettings` reads the overrides one call later).
The 768 GB override reaches the config struct, but never the already-open vault.

**2. The shard count is derived from the quota and pinned at creation.**
`shardCountForQuota(quota)` picks a power-of-two shard count (one shard per
~7 GB, `minShards` 8, `maxShards` 1024) and `loadOrCreateManifest` records it in
`manifest.json` only when the directory is new; later opens reuse the recorded
count and **ignore the quota entirely**. So even if the override reached `Open`,
the layout would not change.

**3. The default quota freezes every fresh node at the floor.** A bare-metal
install boots with the 1 GB default before an operator sets a real quota, so
`shardCountForQuota` returns the floor of 8 shards and pins it forever. 768 GB
of corpus would then ride on 8 shards — ~96 GB per file against a ~7 GB target,
far past what a single bbolt file should carry.

The operator's framing is correct: **the shard count is frozen at first boot by
the initial quota and can never grow.** For a store meant to accumulate a large
corpus over months starting from a small default, a layout that cannot follow
the data is defective by design. Making the quota merely live-mutable would fix
the number on the Overview while leaving 768 GB on 8 giant shards — the same
defect.

What makes a fix cheap: routing today is `hash % N` with `N` always a power of
two, only ever doubled. For that shape the mod-doubling identity holds —
`h mod 2N ∈ { h mod N, (h mod N) + N }` — so doubling `N` splits each shard `i`
cleanly into `i` and `i + N` and touches only the keys that move. Growth is a
local split, not a global reshuffle.

## Decision

Grow the shard pool dynamically as data accumulates — driven by fill, not by the
quota — and make the quota a live soft admission and eviction target fully
decoupled from the layout. Four slices sit behind this ADR.

### A. Linear hashing: routing generalized, existing vaults unchanged

Model the pool as a linear hash table with state `(level L, split s)`, holding
`2^L + s` shards. Routing rehashes only the buckets already split this round:

```
idx = hash & (2^L - 1)
if idx < s { idx = hash & (2^(L+1) - 1) }
```

With `s == 0` this is exactly today's `hash % 2^L`, so an existing vault whose
manifest records `shards: N` (a power of two) *is already* the valid state
`(log2 N, 0)` — it adopts the scheme with no data migration, and a fresh vault
records its initial `(level, 0)` unchanged. Routing and persistence generalize
here with no change to the initial layout; dropping the quota→count coupling
lands with the growth trigger (slice C), so this slice is behaviour-preserving.

The manifest gains the two fields: version 2 records `{version, level, split}`;
a version-1 `{shards: N}` is read as `(level = log2 N, split = 0)` on open.

### B. The split step: compaction-style, exclusive-gate, crash-safe

One split relieves the pointer shard `s` into a new shard `n = 2^L + s`: the keys
in `s` whose extra hash bit now selects `n` are copied into a fresh bbolt file,
the manifest is flipped to the new `(L, s)`, and the moved copies are deleted
from `s`. The pointer advances; at `s == 2^L` the level rolls (`L++`, `s = 0`).

Concurrency reuses the compaction precedent (ADR-0036 C): the split runs under
the engine's exclusive `globalGate` so no transaction is in flight while the
layout changes, one split per pass, the gate released between passes. A bbolt
file tolerates a transient overshoot past the ~7 GB soft target, so nothing on
the write path is latency-critical.

Crash safety follows from the copy → flip → cleanup ordering, with the new
shard durably closed (fsync) before the atomic manifest rename:

- **Crash before the flip.** The manifest still names the old `(L, s)`, so the
  half-written `n` is unrouted and ignored; `s` still holds every key. The split
  is simply retried, overwriting the orphan `n`.
- **Crash after the flip, during cleanup.** `n` holds the moved keys and routing
  now sends them there; the not-yet-deleted copies left in `s` are inert — never
  routed to — a bounded space leak, not a correctness fault. Cleanup is
  idempotent and resumable: delete the keys in `s` whose route is no longer `s`.

So a split is atomic at the manifest flip and self-healing on either side of it.

### C. Fill trigger: paced, off the hot path

A maintenance sweep — modeled on the compaction loop
(`internal/yagonode/compaction_loop.go`) — checks load once per interval and,
while the mean shard load `UsedBytes / (2^L + s)` exceeds the ~7 GB target (with
hysteresis), performs one split per tick. Uniform `xxhash` distribution keeps
shards even, so the round-robin pointer sweep halves them all and converges; a
hot shard is relieved within one sweep of the pointer.

Growth is inherently gradual because it tracks accumulated bytes, not a
configured number — there is never a mass-split, even when the quota jumps,
because the layout follows the data. A node setting `storage.autosplit` (env
`YAGO_STORAGE_AUTOSPLIT`, default on, `off` freezes `N`) gates it, reusing the
compaction setting's live-apply and Storage-tab placement.

This slice also drops the quota→count coupling: a new vault starts at the
`minShards` = 8 concurrency floor regardless of quota and `shardCountForQuota`
is removed, since the fill trigger now sizes the pool.

### D. Quota becomes a live soft admission target

With the layout fill-driven, the quota no longer touches sharding. `engine`'s
`quotaBytes` becomes an `atomic.Int64` behind a `SetQuotaBytes`, and `vault.Vault`
gains `SetQuota`. After `loadRuntimeSettings` applies the `storage.quota`
override at boot, the node calls `vault.SetQuota(config.StorageQuotaByte)`; the
eviction sweep and `AtCapacity` already read `QuotaBytes()` live each cycle, so
the new target takes effect immediately — no restart, no reshard — and the
Overview reads the live figure. The env-before-settings ordering (defect 1)
dissolves: a mutable post-open target makes applying the override after settings
load correct by construction.

The target accounts for logical live rows in the main sharded vault. It is not
a filesystem or aggregate data-root quota: Bleve, the node crawl database, the
crawler frontier, allocated free pages, open-but-deleted blocks, and temporary
split, compaction, migration, and merge copies are outside it. Capacity checks
are advisory preflights and may race ordinary writes. Exact aggregate
enforcement belongs to an operator-provisioned filesystem or project quota, or
a quota-capable volume.

## Consequences

- The shard pool grows with the corpus. A node that boots at the 8-shard floor
  on the 1 GB default and is later given 768 GB splits toward ~128 shards as it
  fills — no operator action, no data migration, no reshard tool.
- The admin `storage.quota` soft target works and applies live.
- Growth briefly quiesces the engine per split — one at a time, bounded, the same
  trade-off compaction already makes and documents; `storage.autosplit off`
  freezes the layout for operators who want a fixed `N`.
- Existing vaults adopt linear hashing as state `(log2 N, 0)` with no migration;
  the manifest gains `level`/`split` and auto-converts a version-1 file.
- No wire-format or peer-protocol change — sharding is an internal storage
  detail invisible to peers and the YaCy API.
- Skew caveat: a pathological key distribution can transiently overshoot the soft
  target between pointer sweeps. bbolt tolerates the larger file and the sweep
  converges; the target is soft by design.

## Alternatives considered

- **Reshard on quota change** (repartition once when the quota grows). A bounded
  single pass, but it ties the layout to a knob the operator must set correctly
  up front, re-introduces an O(data) stall on every change, and does nothing for
  a node that simply crawls past its initial quota-derived `N`. Rejected in favor
  of fill-driven growth.
- **Fixed large shard pool** (always `N` = 256/1024). Zero migrations and a freely
  mutable quota, but it pays hundreds of file descriptors and open/startup cost
  even for a 1 GB node and caps the store at `N` × 7 GB. Rejected: it punishes
  small nodes and re-introduces a ceiling.
- **Extendible hashing** (a persisted directory with per-shard local depth). Lets
  the split target exactly the full shard instead of the round-robin pointer, but
  needs a persisted directory and a larger routing rewrite; linear hashing's
  `(L, s)` is two integers and existing vaults are already valid states. Rejected
  as more machinery than the file-per-shard model needs.
- **Online split without the global quiesce** (snapshot copy plus a dual-written
  delta). Shortens the per-split stall but requires the transaction layer to
  dual-route writes during an in-flight split — invasive on the hot path for a
  rare operation. Deferred: start with the proven compaction-style exclusive
  split, measure, and add online splitting only if the stall is shown to matter.
- **Live-mutable quota without dynamic sharding** (slice D alone). Corrects the
  Overview figure but leaves 768 GB on 8 giant shards — the exact layout defect
  the operator identified. Rejected as a half-fix.
