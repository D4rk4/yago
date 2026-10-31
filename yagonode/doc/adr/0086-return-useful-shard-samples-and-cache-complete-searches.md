# 0086. Return useful shard samples and cache complete searches

Date: 2026-10-23

## Status

Accepted for implementation on 2026-10-23.

## Context

The deadline-bounded randomized shard fallback can produce useful results while
leaving some documents unvisited or sampling only part of a large shard. Treating
that expected coverage limitation as a local-search failure hides usable
results. Repeating the same query can also repeat the same partial walk before a
more complete result is available.

## Decision

As soon as the first useful sampled shard window is available, return its
requested result page and cancel the redundant randomized walk. Preserve its
lower-bound total and incomplete coverage metadata, but do not attach a local
failure solely because the sample is partial. Keep genuine backend failures
visible.

Start one independently bounded full-shard completion for the same query, with
one completion process-wide and a 30-second task deadline. Run detached
completion as local-only work with web fallback disabled, so it cannot fan out
to peers or an external provider after the caller has gone away. Cache only
complete results under the local request identity, within the existing
five-minute expiry and 32 MiB session-cache bounds. A later local page-one
request can reuse the completed result; global requests retain their sampled
response rather than presenting local completion as a complete federated
answer. Requests arriving while a local completion runs may reuse its sample.
Streaming callers receive the completed result only when the local request's
completion finishes inside their existing request deadline.

Report incomplete coverage separately from source or backend failure on the
YaCy progress surface, HTML, and the public portal. Keep the Tavily-compatible
`/search` response shape and availability contract unchanged. A sampled empty
window must not be rendered as a confirmed empty search. Exact queries and
requests excluded from progressive retrieval retain their existing search
path.

## Consequences

First useful results can return before the sampled walk finishes. One bounded
local-only full search may consume additional CPU and I/O after the initiating
request has returned; the process-wide single-task limit and independent
timeout bound that work. An incomplete completion is never promoted to the
complete-result cache. The session cache remains process-local and is lost on
restart. No index format, Tavily wire schema, crawler behavior, or runtime
dependency changes.

Acceptance requires tests for early sample return, full-search coalescing,
complete-cache reuse, rejection of incomplete cache entries, streaming
refinement, honest empty answers, and visible distinction between coverage and
backend failures. The browser flow must be exercised in Chromium and Firefox.
