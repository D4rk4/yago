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
- **Crawl search-index writes** — successful documents, failed write attempts,
  and one duration observation per Bleve batch or fallback write attempt
  (`crawl_search_index_documents_total`,
  `crawl_search_index_write_failures_total`,
  `crawl_search_index_write_duration_seconds`). A failed attempt contributes no
  successful document count, and these series carry no URL or error labels.

## Registration

Every collector registers on the shared registry at startup. A test constructs
all collectors on one registry and gathers it, so a duplicate metric name is
caught as a build-time regression rather than a startup panic in production.

## Admin history and System Monitor

The Admin console keeps a volatile ninety-point ring sampled every ten seconds.
It derives request, error, latency, DHT, queue, process CPU, process RSS, host
memory, and storage series from one registry gathering plus one `sysinfo` host
memory snapshot per interval. The initial gathering runs at sampler startup to
seed counter baselines; the first current System Monitor reading therefore
appears after about ten seconds. Console requests and ten-second HTMX refreshes
read the bounded ring and do not gather metrics or inspect the host directly.

The host-memory display pairs `sysinfo` total RAM with Linux
`/proc/meminfo` `MemAvailable`, the kernel estimate of memory available to new
applications without swapping. Its used value is `total - available`, so
reclaimable page cache is not presented as memory pressure. The parser reads at
most 1 MiB, accepts one decimal `MemAvailable` value in `kB`, rejects malformed,
duplicate, overflowing, or internally inconsistent values, and falls back to
the conservative `sysinfo` free-RAM value only when the field or file is absent.
Process RSS and host-memory meters use total host RAM as their maximum, while
their text retains the actual bounded values. Missing, stale, non-finite, or
invalid observations appear as `Unavailable`. This history is lost at restart;
a Prometheus server remains the durable history source.

When the node crawler runtime is enabled, the monitor also reads the broker's
concurrency-safe heartbeat snapshot. It reports busy fetch-worker jobs against
the effective maximum fetch concurrency per connected crawler. A worker slot
stays busy through page fetch, parsing, and result publication, so the numerator
and configured denominator describe the same bounded pipeline resource. An
enabled runtime with no connected crawler hides the row because no worker
capacity exists to measure; a mixed or older connected fleet that has not
reported the optional measurement reports `Unavailable`. Multiple crawlers use
their aggregate capacity and retain the crawler count and per-crawler limit in
text. Disabling the crawler runtime also removes this row. This live value is
not retained in the ten-second history and does not trigger a metrics gathering
or host scan.
