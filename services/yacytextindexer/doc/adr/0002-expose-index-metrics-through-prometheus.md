# 2. Expose index metrics through the Prometheus client

Date: 2026-07-09

## Status

Accepted

## Context

Indexing behavior must be observable through machine-readable metrics: pages received,
indexed, disposed per reason, index failures, and index latency. The indexer is the one hop
between a crawled page and a searchable document, so a stall here is otherwise invisible. The
sibling services already serve `/metrics` in Prometheus format.

## Decision

We use `github.com/prometheus/client_golang` (pinned in `go.mod`) with a private registry and
serve it on an ops mux alongside `/health`. The page consumer depends only on its own narrow
`IndexProgress` port; `indexmetrics` implements that port.

## Consequences

Metrics aggregate across instances with standard tooling. The consumer stays free of the
metrics vendor.
