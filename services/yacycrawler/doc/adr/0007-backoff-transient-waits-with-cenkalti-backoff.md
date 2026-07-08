# 7. Back off transient waits with cenkalti/backoff

Date: 2026-07-06

## Status

Accepted

## Context

The core waits out transient publication backpressure for as long as the run holds its order,
and retries transient fetch failures a bounded number of times. Both need context-aware,
jittered exponential backoff.

## Decision

We use `github.com/cenkalti/backoff/v4` (pinned in `go.mod`) in the crawl core for publish
backpressure waits and transient fetch retries. Publish waits use an unbounded elapsed time so
they persist until the stream accepts or the context ends.

## Consequences

Backoff behavior is one dependency, already present in the repository graph. The core's clock
abstraction keeps the waits testable.
