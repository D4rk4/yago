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

The endpoint can also be turned off entirely: `YAGO_METRICS_ENABLED` defaults to
`true`, and setting it to `false` unmounts `/metrics` so it returns 404 while the
collectors still run harmlessly in the background.

## Metric groups

The registry publishes:

- **Runtime and process** — Go heap, allocation, garbage-collection, goroutine,
  resident-memory, virtual-memory, CPU-time, file-descriptor, and process-start
  families from the official Prometheus Go and process collectors
  (`go_*`, `process_*`). These are the primary signals for memory-pressure and
  pre-OOM alerts.
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
- **Crawl ingest** — batches absorbed, batches deferred back to the queue, and
  the extracted content bytes, URL rows and postings absorbed as the node ingests
  results from the crawl fleet (`crawl_ingest_batches_total`,
  `crawl_ingest_deferrals_total`, `crawl_ingest_content_bytes_total`,
  `crawl_ingest_urls_total`, `crawl_ingest_postings_total`).

## Registration

Every collector registers on the shared registry at startup. A test constructs
all collectors on one registry and gathers it, so a duplicate metric name is
caught as a build-time regression rather than a startup panic in production.
