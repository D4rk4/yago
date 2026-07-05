# searxng-result-router — Technical Specification

## Context

A SearXNG results page links each result straight to its destination, so `yacyopencrawl`
never learns which result a person opened. `searxng-result-router` is a SearXNG plugin that
rewrites each result link to route through `yacyopencrawl` first and on to the original
destination. It runs inside the operator's own SearXNG instance and depends on no part of
`yacyopencrawl` beyond the links it issues.

## Non-Goals

* Redirecting the browser or fetching any page itself.
* Producing or acknowledging crawl orders.
* Deciding which pages are worth crawling.
* Filtering, reordering, or otherwise changing which results SearXNG shows.
* Rewriting links outside a SearXNG results page.

## Functional Requirements

* The plugin SHALL rewrite every result link on a results page to a link that routes through
  the configured `yacyopencrawl` before reaching the original destination.
* The plugin SHALL leave a result unchanged if it cannot rewrite its link.
* A rewritten link SHALL still lead the person to the result's original destination.
* The plugin SHALL let operators configure the `yacyopencrawl` that rewritten links route
  through.

## Non-Functional Requirements

* The plugin SHALL add no observable delay to result rendering beyond the link-rewriting
  itself.
* The plugin SHALL add no persistent state and no per-request state beyond the page it
  rewrites.
* An operator SHALL be able to add and enable the plugin in an existing SearXNG instance
  without rebuilding SearXNG.

## Known Limitations

* Rewriting applies only where the plugin runs; results from a SearXNG instance without it
  link straight to their destinations.
* The plugin cannot tell whether `yacyopencrawl` is reachable before rewriting a link; an
  outage there breaks every rewritten result until an operator notices.
* Rewritten links are specific to `yacyopencrawl`'s link format; pointing this plugin at a
  differently shaped target requires changing the plugin, not just its configuration.
