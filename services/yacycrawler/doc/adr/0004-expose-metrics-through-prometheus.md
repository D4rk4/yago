# 4. Expose crawl metrics through the Prometheus client

Date: 2026-07-06

## Status

Accepted

## Context

Operational behavior must be observable through machine-readable metrics: orders, pages
fetched and published per output, disposals per reason, refusals honored, publication waits,
and fetch latency. The sibling `yacynode` already serves `/metrics` in Prometheus format.

## Decision

We use `github.com/prometheus/client_golang` (pinned in `go.mod`) with a private registry and
serve it on the ops mux `/metrics`. The crawl core depends only on its own narrow
`RunObserver` port; `crawlmetrics` implements that port.

## Consequences

Metrics aggregate across instances with standard tooling. The core stays free of the metrics
vendor.
