# 0067. Bound persisted search read amplification

Date: 2026-08-27

## Status

Accepted

## Context

Production v0.0.44 held 559,387 searchable documents in eight Scorch shards.
The active index occupied 2,718,265,025 bytes and exposed 69 zap segments,
with eight to ten segments per shard. A never-before-used common query exhausted
the 1.8-second interactive budget while causing 11,240 minor page faults and no
major fault or physical read. Its immediate repeat completed in 95 milliseconds.
The remaining cold cost was mmap page traversal across the persisted segment
fan-out.

A disposable production-sized fixture stored 800,000 documents in eight shards,
occupied 2,660,404,561 active bytes, and produced ten segments per shard.
Three synchronized 100-query cold bursts had p99 latency of 2.243, 1.472, and
1.566 seconds; 27 requests in the first burst exceeded 1.8 seconds. Merging one
shard at a time produced one segment per shard, retained the logical document
totals, reduced active bytes to 1,220,107,628, and used at most 1,076,472 KiB
RSS. Three later 100-query cold bursts had p99 latency of 0.549, 0.841, and
0.305 seconds with no budget failures.

The healthy disk path also requested four times the visible candidate count on
every query so a missing stored document could be replaced without another
Bleve call. On the consolidated fixture with eight CPUs, requesting exactly 100
candidates reduced synchronized 200-query p99 latency from 1.91-2.07 seconds to
1.33-1.59 seconds. The expanded window is necessary only when an orphan occurs
and Bleve reports an unseen tail.

The same 200-query workload with four CPUs still took 2.22-2.44 seconds after
both optimizations, with 41-69 requests exceeding the budget. This excludes a
larger queue or a wider admission gate as an honest single-replica solution.

A release-gate rerun through the completed implementation used a different
800,000-document corpus, again with ten segments in every shard. The production
constructor consolidated it to one segment per shard without changing the
document total. After cache eviction, three 200-query bursts on eight CPUs had
p99 latency of 0.916-0.999 seconds with no failures; 200 queries on four CPUs
had p99 latency of 1.389 seconds with no failures. The different four-CPU
outcomes demonstrate that document count alone is not a capacity model: corpus
shape and persisted bytes remain part of the deployment measurement.

The tail-latency and admission-control results in
[The Tail at Scale](https://research.google/pubs/the-tail-at-scale/),
[arXiv:2312.15123](https://arxiv.org/abs/2312.15123), and
[arXiv:1704.03970](https://arxiv.org/abs/1704.03970) support capacity-backed
admission and bounded first-stage retrieval rather than unbounded queued work.

## Decision

Keep the existing eight-shard index format. Before the disk index becomes
available, inspect each shard's live document and root file-segment totals. If a
shard exceeds one segment per 100,000 documents, measure the complete current
index footprint and pass that amount through the existing filesystem-reserve
admission. Refuse startup before writing when the required temporary growth is
not admitted.

Force-merge one shard at a time. Use the established 100,000-document output
ceiling with a one-document exclusive-bound adjustment required by the Scorch
planner. Reinspect after every merge. The document total must remain unchanged.
If the planner cannot reach the preferred total, accept at most one root segment
per 50,000 documents; refuse startup when an unbounded segment total stops
falling. A shard already inside the preferred bound is unchanged and performs
no storage preflight or merge.

For ordinary disk retrieval without facets or stored-document post-filters,
request exactly the visible candidate count, bounded by the existing hit and
corpus limits. Return that result unchanged when all candidates exist. Only a
confirmed orphan together with an unseen Bleve tail triggers one retry with the
established fourfold bounded window. An orphan with no tail performs no retry,
and retry failure or cancellation remains an ordinary search failure. Complete
search scans retain their existing independently bounded path.

Treat the interactive admission limits as protection for one process, not as a
claim that one replica can complete every possible burst. A deployment target
is valid only up to the cold, synchronized request capacity measured on its
actual CPU allocation and corpus with the full response deadline included.
Serving hundreds of requests within that deadline requires enough independently
validated read capacity to keep every replica below its measured bound. A
future replicated-read service boundary requires its own ADR and synchronized
Docker, systemd, and package topology; this decision does not invent that
boundary or claim that the current four-CPU production replica already meets it.

## Consequences

Cold searches traverse fewer persisted files and healthy queries ask Bleve for
no candidates that the caller cannot use. Rare orphan repair can perform a
second bounded search, preserving the established replacement behavior.

The first start of an existing fragmented index takes additional time and
temporary disk space before listeners open. Consolidation is sequential across
shards and runs before public query work can compete with it. A low-space node
fails with an actionable startup error while retaining the pre-merge snapshots.
Later starts of an already bounded index do no merge work.

Ordinary ingest can create new segments again; a later restart reasserts the
bound. This release does not add online replica synchronization or promise a
hundreds-request SLO on hardware whose measured capacity is lower. Tests cover
exact-page success, orphan retry, no-tail behavior, cancellation and failure,
storage admission in both directions, unchanged shards, sequential progress,
stalled merge refusal, logical document preservation, search preservation, and
reopen integrity. No stored format, dependency, setting, environment variable,
listener, service, image, package topology, or wire shape changes.
