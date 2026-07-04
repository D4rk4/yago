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

- It triggers only on a true miss — after both local search and federated
  peer/cache search return zero results for the request window.
- It uses a DDGS/DuckDuckGo-family metasearch backend, which is keyless. DDGS is a
  Python library and a rate-limited, unofficial scraper, so the Go node ships its
  own `WebSearchProvider` implementation; the concrete backend (an in-house
  `html.duckduckgo.com/html` + `lite.duckduckgo.com/lite` client with `auto`-style
  backend fallback, or a vetted third-party Go dependency that would need its own
  ADR) is chosen when TAVILY-04 is implemented.
- Its results carry an internal `ddgs` provenance so external hits are never
  confused with owned local or federated hits. The provenance is a shared
  mechanism with surface-specific presentation: the human search surfaces (the
  public search portal, the admin search UI, and the `/yacysearch.*` endpoints
  they call) render it as a visible `[ddgs]` marker, while the Tavily-compatible
  `POST /search` API strips it and returns the same results unmarked and
  Tavily-shaped, so that surface stays a drop-in replacement for the Tavily
  Search API. The Tavily surface is search-only and does not browse result pages.
- Discovered URLs the node has never seen are handed to the crawler through the
  existing crawl-order queue (TAVILY-06), using a conservative, robots-respecting,
  egress-guarded profile with per-host caps and URL deduplication, so the next
  identical query is answered from the local index.

The fallback is disabled by default, flipped through admin config
(`YAGO_WEB_FALLBACK_*`), and gated by the SEC-05 privacy mode (off /
explicit-per-request / enabled). Outbound queries pass the in-process egress
guard; responses are rate-limit backed off and briefly cached.

## Consequences

The node no longer depends on a paid, keyed external search API, and never sends a
user query outside the node by default. When an operator opts in, misses become
useful — both by returning `[ddgs]`-marked results on the human search surfaces
(the Tavily drop-in API returns them unmarked) and by seeding the crawler so the
index grows toward the operator's real query traffic.

The costs are external: DuckDuckGo/DDGS is an unofficial, rate-limited surface with
its own terms of service, so the provider must back off on `202 Ratelimit`, cache,
and degrade to an empty result rather than fail the request. Search-miss crawl
seeding is an amplification vector, so it stays conservative and deduplicated and
respects robots, egress deny, and per-host caps. Both behaviors are opt-in and
carry explicit UI indicators. The `WebSearchProvider` seam keeps the concrete
backend replaceable without touching the search core.
