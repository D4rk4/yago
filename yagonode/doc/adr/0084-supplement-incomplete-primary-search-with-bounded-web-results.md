# 0084. Supplement incomplete primary search with bounded web results

Date: 2026-09-07

## Status

Proposed. This document does not enable supplementation in the running node.

On acceptance, amends the retrieval trigger and timing policy in
[ADR-0019](0019-ddgs-web-search-fallback.md) and
[ADR-0021](0021-in-house-metasearch-backend.md).

## Context

An empty-result trigger cannot supplement a weak primary answer containing only
a few useful pages. Sequential local, peer, recovery, and web work can also
consume the response deadline before an external answer arrives. Increasing an
individual HTTP timeout cannot create time beyond the parent request deadline.

Two different quantities need separate limits: the number of verified candidates
available for ranking, and the number of results requested for the displayed page.
An index's estimated total does not establish how many distinct, admissible
results the current request actually has.

## Decision

Use DDGS as an optional supplement when the primary candidate window is too
small. Keep the document-backed local index and compatible peer retrieval as the
primary sources. Reuse the existing Go provider, fusion, policy, and admission
boundaries; add no service or dependency.

### Trigger

Let `K` be the number of unique primary candidates collected for the current
query after supported request filters, safety and relevance checks, canonical
URL deduplication, and content-cluster consolidation. Count before pagination;
stop counting at 100. Do not use an estimated index total, the displayed page
length, or the requested `max_results` as `K`.

| Operator mode | Web retrieval |
| --- | --- |
| `disabled` | Never. |
| `explicit` | Only with request consent and `K < 100`. |
| `enabled` | Automatically when `K < 100`. |
| `always` | Starts alongside primary retrieval regardless of `K`. |

Web search remains disabled by default.

Keep local-only requests, unsupported content domains, and first-seen-bounded
requests outside web eligibility. An unfinished primary stage may also have
`K < 100`; supplement it while retaining its failure evidence. This does not
claim that the index contains fewer than 100 matching documents.

The threshold is a candidate-breadth target, not a promise to return 100 URLs.
Keep the existing public result limits, 500-result paging horizon, and at-most-20
normalized web rows. Do not scan the entire index or paginate external engines
to fill the threshold.

### Time budget

The proposed default retains a **1.8-second total response deadline**, measured
before waiting for the interactive execution slot. Reserve **1.0 second for web
retrieval** and **100 milliseconds for final assembly** within that deadline.
The primary decision must therefore finish by 700 milliseconds after request
admission, including time spent waiting for execution. Existing smaller stage
ceilings still apply.

Start web retrieval immediately when an eligible bounded primary stage finishes
with `K < 100`. Do not wait until the 700-millisecond boundary if the decision is
already available. A bounded local recovery may overlap the web branch; it must
not postpone web startup or start another peer fan-out.

The web deadline is the earliest of the caller deadline, the total response
deadline minus assembly reserve, and 1.8 seconds after web startup. Earlier
primary completion therefore gives web retrieval more than its reserved second.
Queueing, engine admission, backoff decisions, HTTP, parsing, and result
validation all spend this same web budget. No retry resets it.

A caller supplying a shorter deadline takes precedence. The reserve is time
allocation, not a promise that an engine or an execution slot will be available.
Return early when work finishes; never add a fixed delay to a fast answer.

An alternative is a 2.8-second total deadline, adding one second to the current
limit while retaining the 1.8-second web ceiling. That improves the opportunity
for slow engines at the cost of slower sparse-query answers and longer occupied
execution slots. This alternative remains an open operator choice; the proposed
default above preserves the existing total latency contract.

### Completion and ranking

Run one bounded engine race. Keep the existing staggered engine starts and
per-engine backoff. Continue after an empty, rejected, failed, or duplicate-only
batch while eligible engines and time remain. Stop on the first validated batch
that adds at least one distinct admissible result, when all eligible engines
finish, or at the web deadline. Do not repeatedly call an exhausted engine or
wait for 100 web results.

Fuse accepted web and primary rankings using the existing reciprocal-rank fusion,
then apply common final ranking, deduplication, diversity, and the requested page
limit. Preserve primary results if web search fails. Preserve completed rows
when an optional stage times out, and cancel unnecessary outstanding work when
the response is complete. A ready web addition can complete the answer and cancel
unfinished optional local recovery. Unfinished work retains its admission until it exits.

An honest complete empty answer remains an empty success. An empty answer with
an unfinished or failed source remains distinguishable through the existing
availability and Tavily error contracts. Internal provenance remains `ddgs`,
human-facing labels remain `web`, and Tavily payloads gain no provider field.
Optional crawl seeding remains asynchronous and independently bounded.

## Consequences

Sparse but nonempty primary searches can gain external results and may take
longer than the current miss-only path. External query volume can increase
substantially. Disabled mode and explicit consent remain effective privacy
boundaries. Implementation must replace the misleading enabled-mode label
`Enabled on search miss`, document the changed disclosure behavior for existing
enabled installations, and synchronize runtime admin settings, configuration
documentation, and deployment examples for any changed configuration surface.

Implement this policy separately from corrections that preserve completed
results or reduce index work. Those corrections do not require changing when a
query is sent outside the node. Keep the earlier ADRs as historical decisions
and mark their amended scope when this proposal is accepted.

Acceptance requires primary sets of 0, 1, 99, 100, and 101 candidates, including
count saturation at 100; duplicates, filtered rows, and inflated totals; every
operator mode and exclusion; slow primary, recovery,
engine and queue paths; successful, rejected, empty and failed web batches;
completed rows at cancellation; unchanged primary rows after web failure; stable
paging; and identical portal/Tavily ordering for equivalent requests. Measure
queue-inclusive p50/p95/p99, web-call frequency, validated-result yield, and
incomplete-empty frequency on a neutral corpus before enabling the new policy.
The constants 100 and one second are product choices to validate, not values
established as optimal by research.

Go's [context guidance](https://go.dev/blog/context) supports propagating one
request deadline through dependent work. Research on
[tail latency in multistage retrieval](https://arxiv.org/abs/1704.03970) supports
bounding candidate work against an end-to-end budget. Research on
[efficient rank fusion](https://arxiv.org/abs/1811.06147) describes the retrieval
quality and computation tradeoff; it does not establish this proposal's threshold.
