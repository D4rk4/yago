# 16. Expose crawler metrics through the Prometheus client

Date: 2026-07-03

## Status

Accepted

## Context

ADR 0011 adopted `github.com/prometheus/client_golang` for the node's `/metrics`
surface. The crawler is a separate process that dials the node over gRPC and has
had no metrics of its own, so an operator could not see crawler fetch volume,
failures, bytes downloaded, robots denials, active jobs, or how many ingest
batches it published. The CRAWL-07 roadmap task requires those `yacy_crawler_*` metrics so
multiple workers and backpressure behaviour can be observed.

The crawler module (`yago-crawler`) did not previously depend on
`prometheus/client_golang`; the dependency lived only in the node module.

## Decision

We reuse `github.com/prometheus/client_golang` in the crawler module, pinned to
the same version the node uses, rather than inventing a second metrics format.

- A crawler-owned collector registers the `yacy_crawler_*` gauges and counters on
  its own `prometheus.Registry` and serves them in the Prometheus text exposition
  format.
- The crawler exposes `/metrics` on an optional loopback IP-literal listener
  configured by `YAGO_CRAWLER_METRICS_ADDR`; wildcard and non-loopback listeners
  are rejected. When the variable is empty the crawler starts no metrics server
  and only collects in memory, so the default deployment opens no new port. A
  trusted tunnel or proxy provides any required remote scrape.
- Fetch, byte, failure, ingest, and active-job counts are observed from the
  pipeline through a small observer seam, and robots denials from the robots
  admission fetcher, so those packages do not import the metrics collector.
- Firefox diagnostics use one fixed-bucket slot-acquisition histogram, one pool
  gauge with only `ready`, `busy`, and `cooling` states, and one failure counter
  with only `slot_deadline`, `cooldown`, `launch`, and `render` reasons. The
  allowed state and reason series are initialized at zero. Unknown reasons are
  ignored, and URLs or raw error text never become labels. The original
  slot-acquisition deadline counter remains available for existing dashboards.

## Consequences

- `github.com/prometheus/client_golang` and its transitive dependencies are now
  pinned in `yago-crawler/go.mod` as well as `yagonode/go.mod`.
- Crawler metrics aggregate in the same Prometheus server as node metrics and
  share the `yacy_` naming convention already used by node series.
- The crawler gains an optional HTTP listener; it stays closed unless
  `YAGO_CRAWLER_METRICS_ADDR` is set, keeping the worker headless by default.
- Browser-pool metrics remain fixed-cardinality regardless of crawl volume,
  target URL diversity, or browser error diversity. Operators can distinguish
  pool queueing, session occupancy/cooldown, and bounded failure classes without
  adding per-target series.
