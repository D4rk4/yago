# 0063. Serve before rebuilding RWI shard filters

Date: 2026-08-17

## Status

Proposed

## Context

ADR-0039 adds one in-memory binary-fuse filter per vault shard. The filter is a
read accelerator for YaCy-compatible RWI prefix scans. It is not authoritative
storage and is not the local full-text engine. A missing or failed filter is
already conservative: it admits the shard and costs an extra seek but cannot
hide a posting.

The current open path nevertheless scans every persisted RWI key and builds all
filters before any HTTP listener opens. This makes availability depend on vault
size and page-cache temperature. Production supplied three distinct results on
the same eleven-shard corpus:

- one v0.0.36 cold initialization took 13,795.803 seconds;
- the immediately repeated warm-cache initialization took 1,678.295 seconds;
- after the corrected Bleve index rebuild displaced vault pages, v0.0.37 had
  spent 3,104 seconds in filter initialization by 2026-08-17 13:06:46 UTC,
  had read 6,164,705,280 bytes, and still had no listener.

At the last observation the service had zero restarts and zero cgroup pressure
or OOM events. Its 6,177,599,488-byte cgroup charge contained only 115,073,024
bytes of anonymous memory and 6,018,424,832 bytes of file-backed memory, almost
all inactive. The delay is therefore a synchronous cold-storage scan, not a Go
heap leak. Caddy returns HTTP 502 for the whole interval because the node has no
listener yet.

Binary fuse filters are compact immutable accelerators
([Graf and Lemire, 2022](https://arxiv.org/abs/2201.01174)). The pinned Go
implementation can save and load them, but serialization alone is unsafe:
writes made after the saved filter would create false negatives unless filter
freshness and the mutable side set were committed with the shard. bbolt read
transactions provide consistent snapshots, but its documentation warns against
long-running read transactions because they retain pages and can obstruct
remapping by a writer
([bbolt transactions](https://github.com/etcd-io/bbolt#transactions)).

## Decision

When implemented, opening a configured RWI vault will allocate one conservative
filter per shard without scanning the RWI bucket. Every conservative filter will
answer “maybe”, so startup cannot skip a shard and cannot lose a result. Vault
recovery, the document store, Bleve generation admission, and readiness checks
remain mandatory; only the optional RWI acceleration phase leaves the listener
critical path.

Filter construction will run as bounded maintenance after serving begins:

1. Install conservative filters before accepting writes.
2. Build at most one shard at a time from short, paged bbolt snapshots. Do not
   retain one transaction for a whole multi-gigabyte shard.
3. Record every concurrent term write in the existing side set from the moment
   the conservative filter is installed.
4. Publish a completed static filter only under the engine's exclusive gate,
   carrying the side set into the replacement. A cancelled or failed build
   leaves the conservative filter in place.
5. Yield between pages when interactive reads or admitted writes need storage.
   Cancellation and engine close must stop and join the maintenance work before
   shard databases close.

The first implementation will not persist filters. Persistence is a separate
follow-up and is admissible only with an exact shard-freshness protocol and
durable side-set coverage. A stale serialized filter must never be trusted.

No environment variable or admin setting is introduced. The policy is an
internal correctness and startup-availability boundary: one worker, one shard,
bounded pages, and conservative fallback. It adds no service, listener, wire
field, storage dependency, or YaCy behavior.

Implementation tests must prove both directions:

- a conservative filter admits an existing posting and a concurrent new term;
- an honest empty scan remains empty rather than becoming an error;
- a published filter admits every stored and side-set term and rejects an
  absent term on the optimized shard;
- cancellation, collection failure, construction failure, and close retain the
  conservative answer;
- opening the runtime vault reaches listener assembly without invoking the RWI
  key collector.

## Alternatives considered

Keep the eager scan. Rejected because optional acceleration has already caused
multi-hour production HTTP outages and cache-dependent startup time.

Disable filters permanently. Safe for correctness and suitable as a fallback,
but it discards the bounded rare-term seek reduction after startup.

Run the existing whole-shard builder in a goroutine. Rejected because it keeps a
long bbolt snapshot, has no cancellation or close ownership, and can race its
replacement with side-set writes.

Load serialized filters at startup. Deferred because the library format does
not establish that the filter covers the current bbolt transaction state. A
file that loads successfully can still be stale and create a false negative.

## Consequences

The node can become healthy and serve local Bleve search while RWI acceleration
is unavailable. RWI reads may seek every shard until maintenance completes;
with the current eleven shards this is bounded and preserves results. Cold
filter work can still consume I/O and file cache, but it no longer determines
HTTP availability and can yield to live traffic.

The 4 GiB objective remains a capacity target rather than a production hard
cap. Removing the eager scan avoids its startup cache spike; separate lifecycle
tests must still inspect steady-state node and crawler memory.

This proposed record does not claim an implementation or release. Code, tests,
operator documentation, feature catalog updates, full verification, and release
evidence remain required before the status can become Accepted.
