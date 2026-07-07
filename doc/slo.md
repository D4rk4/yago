# Service-level objectives (OPS-11)

The node ships ready-made Prometheus rules (`deploy/prometheus-rules.yml`)
implementing the Google SRE Workbook's multiwindow, multi-burn-rate pattern.
Point Prometheus at `/metrics` on the ops listener (admin-guarded — scrape
with a session or a reverse-proxy exemption) and load the rule file.

## Objectives

| SLO | Target | SLI |
|---|---|---|
| Search availability | 99.5 % over 30 d | non-5xx share of HTTP requests on search endpoints |
| Search latency | 95 % ≤ 2.5 s over 30 d | `search_latency_seconds` histogram share under the 2.5 s bucket |
| Crawl throughput | absorbing whenever active | `crawl_ingest_urls_total` rate while `crawl_runs_active > 0` |

## Page-worthy vs ticket-worthy

Fast-burn alerts (14.4× budget over 1 h **and** 5 m) mean the monthly budget
disappears in about two days — they page. Slow-burn alerts (3× over 6 h
**and** 3 d) mean erosion over weeks — they open a ticket. Requiring both
windows filters short blips without missing sustained burns. The crawl-stall
and intake-shedding alerts are ticket-worthy operational hints, not SLOs.

## Tuning

The 2.5 s latency objective matches the SERP's Doherty budget and the
`search_latency_seconds` bucket layout — if you change the objective, pick
another existing bucket boundary (0.5, 1, 5, 10) or add a bucket; a threshold
between buckets makes the SLI interpolate and drift. Single-node operators
who never page can drop the `severity: page` rules and keep the tickets.
