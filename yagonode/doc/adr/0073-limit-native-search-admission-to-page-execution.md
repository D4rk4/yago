# 0073. Limit native search admission to page execution

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0072](0072-rerank-lexical-candidates-within-one-search-snapshot.md).

## Context

Release v0.0.51 completed individual cold searches but failed bounded concurrent
production acceptance. In one representative eight-request result-bearing
burst, four callers exhausted the outer capacity wait and four admitted callers
all reached the 1.8-second deadline. The node used only 0.15 CPU-seconds across
the complete burst, incurred one minor fault, no major fault, and no physical
read. A repeat completed in 125.6 milliseconds after the held operation
returned.

The disk read admission allowed one whole `SearchIndex.Search` call because an
alias query fans across eight Bleve shards while production exposes four
processors. That admission remained held through document-presence reads,
stored-candidate decoding, filtering, and result projection. Those operations
do not consume Bleve search capacity. One late-canceling projection read could
therefore prevent every unrelated request from entering Bleve even while the
processors and storage were idle.

Go already limits simultaneous execution to `GOMAXPROCS`; counting each shard
goroutine as another processor made the admission width smaller than the
available execution capacity. Elasticsearch's official search-routing design
similarly separates replica selection from a per-node concurrent-shard bound
and uses observed response time, execution time, and search queue depth for
adaptive selection. It does not serialize a complete multi-shard request with
all later projection work behind one cluster-wide token. See
[Search shard routing](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/search-shard-routing)
and
[Reading and writing documents](https://www.elastic.co/guide/en/elasticsearch/reference/8.19/docs-replication.html).

Bleve Scorch does not support simultaneous multi-process read-only access while
another process actively writes the same index. Independent processes must not
use one mutable Scorch directory as a shortcut to additional capacity. The
upstream limitation is recorded in
[Bleve issue 1495](https://github.com/blevesearch/bleve/issues/1495).

## Decision

Admit native disk search by individual Bleve page execution. Acquire the
per-index token immediately before `IndexAlias.SearchInContext` and release it
as soon as that call returns. Document count, document presence, stored
candidate projection, filtering, evidence hydration, orphan handling, and
result mapping remain outside this admission. A complete filtered or faceted
scan reacquires a token for each native page and performs its projection work
after releasing that page token.

Set native page capacity to one request per available Go processor, with a
minimum of one. The existing outer interactive-search admission and deadline
remain authoritative for whole request pipelines. Paths that do not enter the
interactive wrapper are still bounded at the index by the processor-sized
native admission. Cancellation before admission and cancellation while waiting
must not enter Bleve or retain a token.

A delayed native page holds only its own token. Other native pages may use the
remaining processor-sized capacity. A delayed projection holds no native token.
Do not widen the outer request gate or the deadline to disguise insufficient
capacity.

ADR-0074 later bounds fuzzy field-dictionary enumeration at the request
context. A timed-out fuzzy recovery therefore returns its native token after at
most the active dictionary step and normal close instead of walking the
remaining vocabulary after its response.

Treat hundreds of concurrent requests as a horizontal capacity requirement.
Replicas require independent processors and independently owned index state;
same-host processes on the four-processor production machine are not
additional capacity. A replicated read runtime still requires its own accepted
ADR and synchronized replication, routing, health, deployment, package,
rollout, and rollback implementation before it can be released.

## Consequences

One blocked or late-canceling request no longer creates a process-wide native
search convoy. Native page work can occupy the processors already available to
the process, while document projection and evidence retain their own request
budget and storage boundaries.

A disposable production-copy probe synchronized 200 complete candidate reads
with four processors. All completed without error in 1.0924 seconds; p50 was
716.3 milliseconds, p95 1.0473 seconds, p99 1.0892 seconds, and the maximum was
1.0893 seconds. The previous same-copy four-worker measurement was 2.879
seconds. This proves the candidate layer's local admission improvement; it does
not include live document evidence and is not an end-to-end production SLO.

Tests prove both sides of the admission boundary: a blocked native page keeps a
second call out of Bleve, a canceled wait retains no token, a released page
admits the next call, and a blocked projection permits another native page.
Complete ordinary SearchIndex tests preserve result and error behavior.

The change adds no dependency, stored format, setting, environment variable,
listener, runtime service, image, package topology, contract, or wire shape.
