# 0025. Shard the vault and search index into bounded compressed files

Date: 2026-07-06

## Status

Accepted

## Context

The vault is a single bbolt file and the search index a single bleve/scorch
directory whose largest zap segments grow unbounded. A 200 GB node is one
200 GB file: it cannot be backed up incrementally, a single corruption loses
everything, and every stored byte is written uncompressed to the SSD. The
operator requirements are: transparent fast compression to save disk space and
SSD endurance, vault files of at most 10 GB and index files of at most 1 GB,
tolerance to the loss or corruption of any single file, and a three-level
directory fanout (`vault/aa/bb/cc/aabbcc.vlt`, `index/xx/zz/yy/xxzzyy.idx`).

Alternatives considered for the storage engine:

- **badger v4** (Apache-2.0, pure Go): built-in per-block zstd and value-log
  files capped under 2 GB, but its MANIFEST is a global failure domain — a
  missing or mismatched SST fails the whole store open (hypermodeinc/badger
  issue #1291), the opposite of the loss-tolerance requirement.
- **pebble** (BSD-3-Clause): per-level compression profiles and configurable
  target file sizes, but the same LSM/manifest coupling (a lost SST is
  `ErrCorruption` for the store; CockroachDB's answer is "kill the node and
  re-replicate", which a single node cannot do), plus a heavy dependency tree
  that links the cgo DataDog/zstd when CGO is enabled.
- **Sharded bbolt with application-level compression** (chosen): bbolt stays
  (ADR-0005, ADR-0010), each shard is an independent failure domain with its
  own meta pages, file naming and fanout are fully ours, and compression
  happens where the value semantics are known.

For the index, the same reasoning: bleve supports operator-side sharding
through `bleve.NewIndexAlias` (scatter/gather search with merged facets, and
BM25 global scoring via presearch since v2.5), and scorch's merge policy is
injectable (`scorchMergePlanOptions`, with `MaxSegmentSize` measured in
documents), so zap files stay bounded without forking bleve. Zap segments are
already compressed (chunked snappy) and our mapping stores no document bodies,
so re-compressing the index adds little; bounding and per-shard rebuild are
what matter.

New third-party runtime dependencies, both already version-pinned in the
module graph as indirect dependencies of bleve:

- `github.com/klauspost/compress` v1.18.6 (Apache-2.0/BSD-3/MIT mix, actively
  maintained, the de-facto pure-Go zstd): `zstd` at `SpeedFastest` for value
  compression. Its frames carry an xxhash64 content checksum, giving every
  compressed value integrity verification for free.
- `github.com/cespare/xxhash/v2` v2.3.0 (MIT, stable): shard routing hashes.

## Decision

1. **Vault sharding (`internal/shardvault`)**: implement the `vault.Engine`
   seam over N bbolt files, N a power of two derived from the storage quota
   (about one shard per 7 GB, minimum 8) and recorded immutably with the
   layout version in `vault/manifest.json`. Route each record by
   `xxhash64(bucket ‖ key) mod N`; the shard file lives at
   `vault/aa/bb/cc/aabbcc.vlt` from the `%06x`-formatted shard id. Reads and
   scans fan out and merge; writes open transactions only on touched shards.
   Cross-shard atomicity is relaxed by design. A feature that spans shards
   writes durable rows before a separate visibility marker, or retains its
   prior identity as a replay marker until dependent index work completes.
   Retries reconcile partial state from the same crawl observation; re-crawl
   and DHT redistribution remain the wider systemic repair, as in YaCy.
2. **Value compression in shardvault**: one shared `zstd.Encoder` at
   `SpeedFastest`; a one-byte format tag distinguishes raw from compressed
   values; values under 64 bytes or saving less than one eighth stay raw with
   a crc32c (stdlib Castagnoli) checksum. Decompression verifies the zstd
   frame checksum. Keys are never compressed, preserving B+tree ordering and
   prefix scans.
3. **Index sharding**: M shard indexes (a power of two derived from the same
   quota, minimum 4) at `index/xx/zz/yy/xxzzyy.idx`, each opened with
   `scorchMergePlanOptions` tuned so zap files stay under 1 GB
   (`MaxSegmentSize` in documents, measured from observed bytes/doc) and
   `numSnapshotsToKeep: 3`; a `bleve.NewIndexAlias` fans searches in, so
   callers keep one `SearchIndex`. Documents route to shards by
   `xxhash64(documentID) mod M`.
4. **Loss tolerance**: a shard that fails to open or fails its checksums is
   quarantined (renamed aside), recreated empty, counted in metrics, and
   served degraded — the store keeps running on the surviving shards. A
   quarantined index shard rebuilds from the vault's stored documents (the
   existing rebuild path, narrowed to one shard).
5. **Migration**: on startup, a legacy single-file vault streams into the
   sharded layout (compressing on the way) and the legacy index rebuilds from
   the vault; the old files are kept as `.migrated.bak` until the operator
   removes them.

## Consequences

- Any single vault or index file can be lost with bounded damage (1/N of the
  keyspace or 1/M of the index), satisfying the resilience requirement; the
  trade is the loss of cross-bucket transactional atomicity, documented at the
  `shardvault` seam.
- zstd-fast value compression cuts disk usage and SSD write volume for every
  stored page; B+tree batching already amortizes page rewrites, so the
  write-amplification profile stays competitive with LSM engines without
  their manifest coupling (WiscKey, FAST'16; arXiv:2202.04522).
- Two indirect dependencies become direct and are recorded here with pinned
  versions and licenses; no new supply-chain surface is added.
- Directory fanout keeps every directory small and gives backup and scrub
  tooling bounded units (the OpenStack Swift pattern).
- Follow-up: STOR-02 implements shardvault, STOR-03 the index sharding,
  STOR-04 the quarantine and integrity checks; an offline `reshard` command
  covers future N changes.
