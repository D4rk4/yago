# 21. In-house multi-engine metasearch backend for the DDGS fallback

Date: 2026-07-03

## Status

Accepted

## Context

ADR-0019 chose an optional, admin-toggled DDGS web-search fallback over a paid
upstream provider, and left the concrete backend to the implementation. The DDGS
family is a keyless metasearch idea: rotate queries across several public search
engines. Two concrete decisions were open: which engines, and whether to build
the client in-house or take a third-party Go dependency.

The obvious first choice — DuckDuckGo's own `html`/`lite` endpoints — turns out to
be the wrong default. DuckDuckGo aggressively rate-limits and blocks automated
queries (it answers with `202 Ratelimit` quickly), so leaning on it for the
default `auto` backend would make the fallback fail in practice.

The node has no HTML parser of its own. `golang.org/x/net` is already in the
module graph as an indirect dependency (v0.56.0); its `html` package is what the
crawler already uses for extraction.

## Decision

Build the provider in-house as a small, structure-driven multi-engine metasearch
client (`yacynode/internal/websearch`), and promote `golang.org/x/net` (v0.56.0,
BSD-3-Clause) to a direct dependency of `yacynode` for HTML parsing.

Engine selection:

- The default `auto` backend deliberately **excludes DuckDuckGo**. It queries
  keyless, scraper-tolerant engines in order — Mojeek (an independent index with a
  stable, direct-link result list) first, then Bing — and returns the first
  engine's non-empty results.
- DuckDuckGo (`html` then `lite`) is available only when an operator selects it
  explicitly (`YAGO_WEB_FALLBACK_BACKEND=duckduckgo`), with the understanding that
  it rate-limits hard.
- Parsing is structure-driven rather than pinned to fragile class names: Mojeek and
  Bing share one list parser (`<li>` container, `<h2><a href>` title, `<p>`
  snippet, direct URLs); DuckDuckGo has its own parser that unwraps the `/l/?uddg=`
  redirector.

Resilience is built into the provider, not the caller: it caches responses briefly
(bounded, TTL), backs off exponentially on `202`/`429`, and degrades to an empty
result on rate limiting or a backend error rather than failing the search.
Outbound requests go through the egress-guarded HTTP client (ADR-0013).

## Consequences

The node depends on no paid or keyed search API and ships a single Go appliance
with no extra runtime service. Scraping public result pages is inherently brittle:
an engine can change its markup or block the client. This is mitigated by
structure-driven parsing, the multi-engine `auto` list, backoff, caching, and
degrade-to-empty, and the engine list is easy to extend. The fallback is
best-effort by design and off by default; when it fails, the search simply returns
the original miss. `golang.org/x/net` becomes a direct dependency, pinned in
`go.mod` and already vetted through the crawler's use of the same package.
