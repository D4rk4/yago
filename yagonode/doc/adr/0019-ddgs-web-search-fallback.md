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

- It runs only when the operator permits it. Privacy mode `enabled` authorizes
  eligible global queries, `explicit` requires the individual request to consent,
  and `disabled` does not install web search. The separate start trigger defaults
  to `miss`, after exact/morphological local-plus-peer retrieval and bounded local
  fuzzy recovery both miss. `parallel` is an explicit operator choice that starts
  web retrieval alongside local and peer work and fuses their completed rankings.
  A local-only request never reaches peers or this provider.
- A Tavily-compatible `/search` call explicitly permits web fallback because it
  is itself a web-search request. Basic depth can therefore use local exact and
  fuzzy recovery before web without enabling swarm fan-out; YaCy
  `resource=local` and admin `scope=local`
  remain strictly local.
- It uses the pure-Go keyless provider selected in ADR-0021. `auto` starts the
  preferred engine immediately, hedges later engines at 50-millisecond intervals,
  and cancels the remaining attempts when one answer survives relevance checks.
- Local and swarm retrieval receive only the parsed search terms and structured
  fields. The fallback submits the bounded original query, including supported
  provider operators, then independently enforces site, TLD, file type, URL, and
  excluded-term constraints that can be verified from each returned row.
- Its results carry internal `ddgs` provenance so external hits are never
  confused with owned local or federated hits. Human renderers expose web
  provenance, while a compatible Tavily-shaped adapter omits project-specific
  provenance.
- Discovered URLs the node has never seen are handed to the crawler through the
  existing crawl-order queue (TAVILY-06), using a conservative, robots-respecting,
  egress-guarded profile with per-host caps and URL deduplication, so the next
  identical query is answered from the local index.

The fallback is disabled by default and installed through admin config
(`YAGO_WEB_FALLBACK_*`). Outbound queries pass the in-process egress guard;
responses are rate-limit backed off and briefly cached under a fixed 4 MiB and
256-entry limit after per-field normalization. Interactive miss-only requests run
the ordered exact local-plus-swarm, local fuzzy, then web cascade inside a fixed
deadline. Parallel mode overlaps the web stage with the primary ranking and
deduplicates the fused result set. Exact, fuzzy, and web stages are capped
independently, and retained exact or fuzzy work holds bounded admission until it
exits, so a context-insensitive local query cannot starve the provider or
accumulate work.

## Consequences

The node no longer depends on a paid, keyed external search API. An operator can
keep all searches local plus peers, require request-level consent, permit
provenance-marked web results after a true miss, or explicitly run bounded web
retrieval alongside every eligible local and peer query.
Fallback results can seed the crawler so the local index grows toward its query
traffic. Durable queue publishing runs after the search response through a
process-wide two-work admission with a ten-second deadline. Saturation drops the
optional seed operation instead of adding search latency or an unbounded queue.

The providers are external rate-limited surfaces with their own terms of service,
so the client backs off, caches briefly, and degrades to an empty result rather
than fail the request. Search-miss crawl seeding is an amplification vector, so it
stays conservative and deduplicated and respects robots, egress deny, and per-host
caps. Background admission also bounds concurrent durable queue writes. Both
behaviors require opt-in. The `WebSearchProvider` seam keeps the
concrete backend replaceable without touching the search core.
