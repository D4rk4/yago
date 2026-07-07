# Observability methodology (OPS-07)

The node's metrics follow two complementary methods on one Prometheus
registry, served at `/metrics` on the ops listener (behind the admin guard;
scrape with a session or a reverse-proxy exemption).

## RED — every request-serving surface

Rate, Errors, and Duration for each HTTP endpoint:

| Signal | Metric |
|---|---|
| Rate + Errors | `http_requests_total{endpoint,status_code}` — errors are the 4xx/5xx series |
| Duration | `http_request_duration_seconds{endpoint}` histogram |

Search adds its own quality series (`search_*` from SearchMetrics), and the
crawler exposes `yacy_crawler_*` on its own optional listener.

## USE — every bounded resource

Utilization, Saturation, and Errors for the node's capacity-bounded parts:

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Document/RWI storage | `storage_*` used-bytes vs quota | eviction activity (`eviction_*`) | write failures surface as request errors |
| Crawl pipeline | `queue_crawl_depth`, `queue_index_depth` gauges | ingest deferrals (`crawl_*` deferred counters) | `crawl_*` rejected counters |
| DHT intake | slot-bounded (fixed) | `intake_rejections_total{gate="dht_transfer"}` | protocol-level busy answers |
| Inbound remote search | slot-bounded (fixed) | `intake_rejections_total{gate="remote_search"}` | — |
| Web fallback engines | — | per-engine backoff (debug log) | provider errors degrade to empty answers |

## Saturation events

`intake_rejections_total` counts every request shed by a bounded intake gate.
A rising rate on a gate means that intake runs at capacity **before** latency
or error rates show it — alert on `rate(intake_rejections_total[5m]) > 0`
sustained for minutes, not on single spikes.

## Follow-ups

OpenTelemetry export and W3C trace propagation belong to the internal-tracing
work (OPS-10); SLO targets and burn-rate alerts to OPS-11.
