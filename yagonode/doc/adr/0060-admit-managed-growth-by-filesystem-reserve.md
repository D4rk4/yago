# 0060. Admit managed growth by filesystem reserve

Date: 2026-07-17

## Status

Accepted

## Context

`YAGO_STORAGE_QUOTA` measures logical live rows in the main sharded vault. It
does not include the Bleve index, the node crawl database, crawler frontier
databases, bbolt free pages, files that remain allocated after deletion, or
temporary copies created by compaction, shard splitting, migration, and index
merges. The node and crawler can also use different filesystems or container
volumes. No process-local directory scan can atomically reserve space across
those independent writers.

The operating system is the final allocator. Linux filesystem and project
quotas, and quota-capable managed volumes, can enforce an aggregate hard
boundary. Kubernetes likewise accounts local ephemeral storage at the node
boundary and documents filesystem project quotas as the accurate tracking
mechanism. Docker volumes deliberately delegate capacity and quota policy to the
volume driver and backing filesystem. YaGo therefore needs early backpressure
before allocation failure, without claiming that application preflight is a
hard quota.

References:

- [Kubernetes local ephemeral storage](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#local-ephemeral-storage)
- [Linux XFS quota administration](https://man7.org/linux/man-pages/man8/xfs_quota.8.html)
- [Docker volumes](https://docs.docker.com/engine/storage/volumes/)
- [bbolt compaction](https://github.com/etcd-io/bbolt#compact)

## Decision

Use a reserved-free filesystem policy as advisory admission control for every
growth path connected to the storage-pressure gate. The node and crawler have
independent policies because their data directories may be on different
filesystems:

- `YAGO_STORAGE_RESERVED_FREE`, default 1 GiB, is the node reserve.
- `YAGO_STORAGE_PRESSURE_HYSTERESIS`, default 256 MiB, is the additional free
  space required before node admission resumes.
- `YAGO_CRAWLER_STORAGE_RESERVED_FREE`, default 1 GiB, bootstraps the crawler
  reserve and is sent to connected crawler workers.
- `YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS`, default 256 MiB, is the crawler
  recovery margin.

Each process measures the filesystem containing its own `YAGO_DATA_DIR` with
`statfs` available blocks. A failed measurement fails closed. Ordinary growth
checks share one observation for at most one second to avoid multiplying
filesystem calls under concurrent ingest. Once pressure is active, admission
does not resume until available space reaches the reserve plus hysteresis.
Changing any of the four values in Configuration applies to the live node and
connected crawlers; environment variables remain bootstrap defaults.

Node-side gate-managed crawl and index ingestion, new crawl orders, optional
remote-result retention, reputation observations, and other connected
best-effort writes pause under pressure. Removal, eviction, deletion, and
settlement paths remain available. The crawler stops new fetch and frontier
growth admission while retaining its committed checkpoint. Search reads and
operator cleanup remain available after an ordinary running node reaches
pressure.

Operations that copy an existing storage source use operation-specific
headroom. Compaction and shard splitting share one maintenance admission lock:
inside that lock they measure the actual copy source, force a fresh filesystem
observation, reserve the required bytes above the configured free-space
reserve, and keep the lock until the copy finishes. Each retained legacy-state
migration page uses the same serialized path with its payload size plus a
bounded bbolt allocation allowance. Legacy single-file vault migration checks
the source file size before startup copying. Initial creation of a missing or
zero-length `crawlbroker.db` reserves 1 MiB; an existing valid crawl database is
opened without a pressure check so recovery and cleanup startup remain
possible.

Bleve rebuilds from the document store remain startup work. When a required
rebuild cannot pass growth admission, startup fails closed before readiness and
reports the pressure error. The operator must free filesystem space or lower
the reserve before retrying; Admin cleanup is not available until the node can
start.

Crawler storage and active-fetch heartbeat observations expire after three
normal heartbeat intervals. An older worker, an omitted field, or a stale
report is shown as unreported or unavailable rather than retaining an obsolete
healthy or busy value.

Prometheus exposes current pressure, available bytes, policy values, rejected
admissions, and measurement failures. Admin surfaces label main-vault values as
logical live data and the main-vault quota as a soft admission target. Pressure
messages state the exact gate-managed work that pauses and direct the operator
to free filesystem space or lower the applicable policy.

## Consequences

- Concurrent crawler and peer ingestion receives early bounded backpressure
  before the filesystem is exhausted.
- Large maintenance copies cannot pass independent stale preflights and then
  overlap each other inside the node.
- Cleanup remains possible under runtime pressure, and an existing crawl state
  remains openable for recovery.
- A startup Bleve rebuild or legacy migration can require out-of-band operator
  recovery because the Admin console is not yet available.
- Deleting bbolt rows can make pages reusable by that database without
  increasing operating-system free space. Compaction or unrelated filesystem
  cleanup may still be required before admission resumes.
- Ordinary writes can allocate between an advisory check and commit. Bleve
  merges, database allocation granularity, open-deleted files, and writers not
  connected to the gate can also consume space. The policy does not provide an
  exact aggregate byte ceiling.
- Operators that require a hard maximum must provision a filesystem or project
  quota, or a quota-capable volume, covering the complete data placement. When
  node and crawler use separate volumes, each volume needs its own limit.

## Alternatives considered

### Treat the main-vault quota as the aggregate limit

Rejected. It cannot account for Bleve, crawl checkpoints, allocated free pages,
or temporary copies and would present a false guarantee.

### Scan the data directory before every write

Rejected. Directory scans are expensive under concurrent crawling, cannot see
all allocated or open-deleted space reliably, and do not make allocation atomic
across processes or volumes.

### Put a shared lock around every node and crawler write

Rejected. A filesystem lock does not reserve blocks, does not cover storage
engine background work, and would serialize unrelated processes on the ingest
hot path. The maintenance lock is intentionally limited to large node-side copy
operations.

### Stop every write under pressure

Rejected. Blocking deletion, eviction, settlement, and recovery would prevent
the system from relieving pressure or completing already committed work.

### Require one storage backend and one deployment layout

Rejected. YaGo must retain identical behavior under Docker, systemd, and Debian
packages, while operators remain free to place node and crawler data on one or
separate filesystems.
