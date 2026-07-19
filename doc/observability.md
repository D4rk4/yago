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

The crawl monitor derives bounded crawl-health signals from each run tally.
After 100 fetched pages, **harvest rate** is indexed/fetched and a running run
below 20 percent is identified as a possible spider trap or junk source. Link
redundancy is duplicates/(duplicates+fetched+failed+robots-denied); it remains
informational and high duplicate volume alone does not trigger a warning. After
100 aggregate fetched-plus-failed outcomes, **failure rate** is
failed/(fetched+failed); a running run above 50 percent identifies one or more
blocking or unavailable hosts. Every rendered share stays within 0–100 percent.
Indexing activity is read from Prometheus as
`rate(crawl_search_index_documents_total[1h])` against the queue depths. A full
age-of-index gauge would require a corpus scan and remains a follow-up.

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

## SLOs and burn-rate alerts (OPS-11)

`deploy/prometheus-rules.yml` ships recording rules and multiwindow
burn-rate alerts for the search availability/latency SLOs and a crawl-stall
hint; `doc/slo.md` explains the objectives and the page-vs-ticket split.
