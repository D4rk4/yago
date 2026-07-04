# Metrics

The node exposes Prometheus-format metrics at `GET /metrics` on the operations
listener (`YAGO_OPS_ADDR`), the same surface that hosts the admin console and the
health probes. Metrics are gathered from a single registry, so every collector
shares one scrape.

## Endpoint protection

`/metrics` is not on the operations listener's public allowlist (which is limited
to the health and readiness probes and the login/setup pages). It therefore
requires an authenticated admin request, exactly like the console: a scraper must
present an admin session cookie or an API key. Bind the operations listener to a
private interface or loopback (see [configuration.md](configuration.md)) so the
metrics and admin surfaces are not publicly reachable.

## Metric groups

The registry publishes:

- **HTTP requests** — per-endpoint request counts, latencies and error classes
  for the served surfaces.
- **Storage** — configured quota and bytes currently used
  (`storage_quota_bytes`, `storage_used_bytes`).
- **Eviction** — URLs and postings purged under quota pressure and sweeps that
  failed (`eviction_*_total`).
- **DHT** — inbound and outbound postings, batches and failures for the
  distributed index exchange.
- **Peers** — connected-peer counts and probe outcomes.
- **Authentication** — admin authentication failures.
- **Queue depths** — the crawl and index backlog read live from the DHT gate
  snapshot at scrape time (`queue_crawl_depth`, `queue_index_depth`).
- **Search** — request count, latency, result count and partial failures,
  metered once at the search composition chokepoint so the YaCy, Tavily and
  portal surfaces share one view (`search_requests_total`,
  `search_latency_seconds`, `search_results`, `search_partial_failures_total`).

Crawl job/fetch/byte metrics are the remaining group from the observability plan
and are not yet published.

## Registration

Every collector registers on the shared registry at startup. A test constructs
all collectors on one registry and gathers it, so a duplicate metric name is
caught as a build-time regression rather than a startup panic in production.
