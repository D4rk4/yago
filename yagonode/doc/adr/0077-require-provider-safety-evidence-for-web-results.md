# 0077. Require provider safety evidence for web results

Date: 2026-08-28

## Status

Accepted

Amends [ADR-0019](0019-ddgs-web-search-fallback.md) and
[ADR-0021](0021-in-house-metasearch-backend.md).

## Context

The Tavily-compatible search request carries a Boolean `safe_search` policy.
The common safety boundary rejects web rows whose safety is unknown. DDGS
results had no safety evidence, so a provider could fetch and accept rows while
the final boundary removed every one. On a fresh isolated node, the same RFC
9562 query returned five web rows with safe search disabled and an incomplete
zero-row response with safe search enabled. No existing database was opened.

The provider also applied only its operator-configured preference. A request
that required safe search could therefore use an engine in `off` or `moderate`
mode. DuckDuckGo documents `kp=1` as strict, `kp=-1` as moderate, and `kp=-2`
as off. Brave documents `safesearch=strict`, and Mojeek documents `safe=1` as
its adult-content filter. The configured Bing HTML path has no implemented
safe-search contract.

Treating every provider row as generally safe would weaken the existing
boundary. Treating a request flag as a promise without changing provider
egress would make the evidence false.

## Decision

A request with `safe_search=true` requires the strongest documented filter on
the selected provider engine. Engine definitions declare whether they can
enforce that request. The engine race excludes an engine without that
capability before egress. An explicitly selected backend with no capable
engine returns a recoverable provider failure and sends no request.

A row fetched under that required filter carries internal provider-filter
evidence through result normalization, the bounded provider cache, constraint
verification, and conversion to the search result. The common safety boundary
admits that evidence only when the row source is external web search. The same
value on a local or peer result is rejected, and an unknown web result remains
rejected.

The request-level requirement overrides the operator's configured provider
preference only for that request. Requests without the flag retain the
configured `strict`, `moderate`, or `off` mode. Provider-cache identity includes
the request-level policy so filtered and unfiltered answers cannot share an
entry.

The evidence stays internal. Tavily-compatible responses retain their standard
shape, and human-facing provenance remains `web`.

## Consequences

Safe-search web fallback can now contribute rows without weakening the
conservative unknown-result rule. Auto mode may skip Bing for a safe request,
so it has fewer eligible engines and can expose a recoverable web partial
failure when the filtered engines are unavailable.

Permanent tests prove request override and ordinary configured mode, corrected
DuckDuckGo parameter values, filtered/unfiltered cache separation, preservation
of provider evidence during normalization, refusal before egress for an
unsupported backend, and rejection of forged provider evidence on local and
peer rows. A fresh full-stack synthetic returned five rows for both safe and
unsafe requests.

The change adds no dependency, stored format, setting, environment variable,
listener, service, image, package topology, public response field, or wire
shape.
