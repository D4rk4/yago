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

## Crawl health (OPS-09)

The crawl monitor derives Olston & Najork's crawl-health signals from the run
tallies once a sample reaches 100 fetched pages: **harvest rate**
(indexed/fetched — how much fetch effort became index entries), **duplicate
rate** (the spider-trap smell; a warning names the run when a running crawl
exceeds 30 % duplicates), and **failure rate** (a blocking or dead host flags
above 50 %). Index freshness is read from Prometheus as
`rate(crawl_documents_indexed[1h])` against the queue depths — a full
age-of-index gauge would need a corpus scan and stays a follow-up.

## Internal tracing (OPS-10)

Every HTTP request roots a W3C Trace Context: a valid inbound `traceparent`
is adopted, otherwise a fresh trace starts here, sampled 1-in-256. The trace
rides the request context; the peer fan-out stamps a child `traceparent` on
every outbound peer search, so one public query correlates across its legs in
the logs of cooperating nodes. Sampled traces attach their trace ID as an
**exemplar** on `http_request_duration_seconds` — Grafana links a slow bucket
straight to a live trace ID. Scope is deliberately this node plus its own
outbound legs; a full OpenTelemetry export would need a collector dependency
(ADR required) and is not planned until an operator asks for one.

## Follow-ups

SLO targets and burn-rate alerts belong to OPS-11.
