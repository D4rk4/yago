# 19. Optional DDGS web-search fallback instead of an upstream Tavily provider

Date: 2026-07-03

## Status

Accepted

## Context

The plan carried an "optional real Tavily upstream provider" (TAVILY-04): an
outbound client to the commercial Tavily Search API, gated by a `TAVILY_API_KEY`,
budget, and circuit breaker, so a node could meta-search a paid service when its
own index came up short.

That direction is unwanted. It adds a paid, keyed, metered dependency on a single
commercial vendor for the one situation the project most wants to improve on its
own terms: a query the node cannot answer from its index or its federated peers.
It also does nothing to make the node better over time — every miss stays a miss.

The node already searches local Bleve first and federated YaCy/yago peers second
(`searchcore.federatedSearcher`), and it already owns a crawler and a durable
crawl-order queue (`crawldispatch.CrawlOrderQueue`). A search miss is exactly the
signal that should feed the crawler.

## Decision

Drop the outbound upstream-Tavily provider entirely. In its place, offer an
optional, admin-toggled **DDGS web-search fallback**:

- It runs only when the operator permits it. Mode `enabled` automatically runs
  after exact/morphological local-plus-peer retrieval and the applicable bounded
  local recovery miss, `always` starts web retrieval alongside local and peer work
  and fuses their completed rankings, `explicit` requires the individual request
  to consent and runs after a miss, and `disabled` does not install web search. A
  local-only request never reaches peers or this provider.
- Every Tavily-compatible search depth uses the global retrieval path and
  permits web fallback according to the operator policy. `basic`, `fast`, and
  `ultra-fast` retain `verify=false`; `advanced` uses `verify=ifexist`. YaCy
  `resource=local` and Admin `scope=local` remain local-only.
- It uses the pure-Go keyless provider selected in ADR-0021. `auto` starts the
  preferred engine immediately, hedges later engines at 50-millisecond intervals,
  and cancels the remaining attempts when one answer survives relevance checks.
- Local and swarm retrieval receive only the parsed search terms and structured
  fields. The fallback submits the bounded original query, including supported
  provider operators, then independently enforces site, TLD, file type, URL, and
  excluded-term constraints that can be verified from each returned row.
- DNS-name and IPv4 include-domain values may add a bounded provider `site:`
  operand. IPv6 literals retain the base query because DDGS does not define an
  unambiguous IPv6 operand; authoritative returned-host filtering still applies.
- Its results carry internal `ddgs` provenance so external hits are never
  confused with owned local or federated hits. Human renderers expose web
  provenance, while a compatible Tavily-shaped adapter omits project-specific
  provenance.
- Eligible surfaced URLs that are absent or lookup-indeterminate are handed to the
  crawler through the existing crawl-order queue (TAVILY-06), using a conservative,
  robots-respecting, egress-guarded profile with per-host caps and active-work URL
  coalescing, so a later query can become local after successful ingest.

The fallback is disabled by default and installed through admin config
(`YAGO_WEB_FALLBACK_*`). Outbound queries pass the in-process egress guard;
responses are rate-limit backed off and briefly cached under a fixed 4 MiB and
256-entry limit after per-field normalization. Interactive miss-only requests run
the ordered exact local-plus-swarm, applicable mutually exclusive local recovery,
then web cascade inside a fixed deadline. `always` overlaps the web stage with the primary ranking and
deduplicates the fused result set. Exact, fuzzy, and web stages are capped
independently, and retained exact or fuzzy work holds bounded admission until it
exits, so a context-insensitive local query cannot starve the provider or
accumulate work.

## Consequences

The node no longer depends on a paid, keyed external search API. An operator can
keep all searches local plus peers, require request-level consent, permit
provenance-marked web results after a true miss, or select `always` to run bounded
web retrieval alongside every eligible local and peer query.
Fallback results can seed the crawler so the local index grows toward its query
traffic. Durable queue publishing runs after the search response through two
background workers, at most 128 pending jobs, and a ten-second deadline per job.
Each worker starts with a fresh bounded context, so queue delay does not consume
that deadline. An absent or lookup-indeterminate URL uses a one-at-a-time
node-internal recovery intent for durable publication. Its normalized identity
coalesces only while the order is pending or leased and is released after
acknowledgement, terminal failure, or cancellation so later discovery can retry.
Successful lease-authorized ingest attempts to persist the live lease's profile
before recording the fetch. A profile or schedule write failure is logged and
cannot roll back indexed content. The root fetch performs the warming, and no
parallel cache order duplicates active work. A full queue drops only the new
optional warming job instead of adding search latency or an unbounded queue.

The providers are external rate-limited surfaces with their own terms of service,
so the client backs off, caches briefly, and records a classified partial failure.
A row-bearing search remains successful; an empty Tavily-compatible response with
that incomplete source is retryable HTTP 503. Web-result crawl seeding is an
amplification vector, so it stays conservative and coalesces active work while
respecting robots, egress deny, and per-host caps. Background admission also bounds
concurrent durable queue writes. Both behaviors require opt-in. The
`WebSearchProvider` seam keeps the concrete backend replaceable without touching
the search core.
