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

DuckDuckGo's `html` and `lite` endpoints offer useful multilingual coverage but
aggressively rate-limit automated queries. A default that waits for each engine
in sequence also multiplies tail latency when an endpoint stalls or returns an
off-topic bot-tier response.

The node has no HTML parser of its own. `golang.org/x/net` is already in the
module graph as an indirect dependency (v0.56.0); its `html` package is what the
crawler already uses for extraction.

## Decision

Build the provider in-house as a small, structure-driven multi-engine metasearch
client (`yagonode/internal/websearch`), and promote `golang.org/x/net` (v0.56.0,
BSD-3-Clause) to a direct dependency of `yagonode` for HTML parsing.

Engine selection:

- The default `auto` backend orders DuckDuckGo HTML, DuckDuckGo Lite, Brave,
  Mojeek, and Bing by expected answer quality. It starts the preferred engine
  immediately, hedges another after 50 milliseconds, immediately replaces an
  empty, failed, or rate-limited attempt, and cancels outstanding requests on the
  first accepted answer.
- Engine-specific selection remains available for operators who prefer one
  provider. Rate-limit backoff is independent per engine, so one blocked endpoint
  does not pause the rest.
- Parsing is structure-driven rather than pinned to fragile class names: Mojeek and
  Bing share one list parser (`<li>` container, `<h2><a href>` title, `<p>`
  snippet, direct URLs); DuckDuckGo has its own parser that unwraps the `/l/?uddg=`
  redirector.

Resilience is built into the provider, not the caller: it caches normalized
responses under a 4 MiB/256-entry byte-aware cache and the configured TTL, retains
at most 20 rows per query with bounded fields, backs off exponentially on
`202`/`429`, and degrades to an empty result when every engine fails rather than
failing the search. The interactive caller caps the complete engine race at 950
milliseconds. A process-wide admission bound allows at most eight active engine
fetch-and-parse attempts; saturated attempts wait only within that existing
caller context. Outbound requests go through the egress-guarded HTTP client
(ADR-0013).

## Consequences

The node depends on no paid or keyed search API and ships a single Go appliance
with no extra runtime service. Scraping public result pages is inherently brittle:
an engine can change its markup or block the client. This is mitigated by
structure-driven parsing, the multi-engine `auto` list, backoff, caching, and
degrade-to-empty, and the engine list is easy to extend. Web search is best-effort
by design and off by default; when it fails, search preserves the primary result
or original miss. Mode `enabled` permits automatic web search after a complete
miss, while `explicit` still requires request-level consent and `always` overlaps
web with local and peer retrieval.
`golang.org/x/net` becomes a direct dependency, pinned in `go.mod` and already
vetted through the crawler's use of the same package.
