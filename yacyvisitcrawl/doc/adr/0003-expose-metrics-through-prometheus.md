# 3. Expose visit metrics through the Prometheus client

Date: 2026-07-07

## Status

Accepted

## Context

Operational behavior must be observable through machine-readable metrics: visits received and
rejected, and crawl orders placed versus unplaced (the rate at which the broker is failing to
keep up). The sibling `yacynode` and `yacycrawler` already serve `/metrics` in Prometheus
format.

## Decision

We use `github.com/prometheus/client_golang` (pinned in `go.mod`) with a private registry and
serve it on the ops mux `/metrics`. `visitintake` depends only on its own narrow `VisitMetrics`
port; `visitmetrics` implements that port.

## Consequences

Metrics aggregate across instances with standard tooling. `visitintake` stays free of the
metrics vendor.
