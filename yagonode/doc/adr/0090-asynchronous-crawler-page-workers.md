# ADR-0090: Dispatch crawler pages through a bounded asynchronous worker pool

## Status

Accepted

## Context

The replacement crawler processed one frontier URL inline in each crawl-order
goroutine. One slow fetch, robots response, parser pass, or node ingest call could
therefore hold that run before its next URL. Its node ingest call also retried
inline, so a stalled node acknowledgment could stop URL progress indefinitely.

## Decision

Use a shared page-worker limit across active crawl runs and keep the active-run
limit separate. Each claimed URL gets its own five-second wall-clock attempt
context. The worker settles or retries the URL when that context expires and
releases its page slot. Per-host admission and politeness continue to apply.

Persist each completed ingest batch in the crawler frontier before settling the
page. A bounded sender pool drains the durable outbox and retries node
submissions independently from page workers. Its size is the shared 64-record
ingest batch ceiling, allowing the node to coalesce concurrent deliveries. A
crawl order remains leased until its outbox entries are acknowledged,
preserving the existing live-lease authorization.
Worker heartbeats include a bounded list of active URLs, stages, and elapsed
times for the Admin crawl pulse.

Run page budgets are enforced in the same frontier transaction that admits a
seed or discovered URL, so parallel page workers cannot exceed a focused crawl's
configured maximum.

## Consequences

Page work no longer waits inline for another URL's fetch or for node ingest
acknowledgment. The shared worker count now controls page concurrency rather
than limiting how many crawl orders can execute. Node ingest itself remains
behind the existing unary `SubmitIngest` contract and its serial consumer. No
new service boundary, dependency, environment variable, or deployment surface is
introduced.
