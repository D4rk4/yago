# yago-crawler2

`yago-crawler2` is the independent source module for the crawler. The deployed
product keeps the existing `yago-crawler` command, binary, service, and image
names. The module does not import runtime code from `yago-crawler`.

The executable connects to the node's crawl exchange, receives and confirms
leased orders, and reports page progress and terminal totals. It keeps a
run-scoped BoltDB frontier under `YAGO_DATA_DIR/yago-crawler2/frontier.db`,
applies robots rules and host politeness, fetches pages through the shared
egress guard, parses HTML and sitemaps, and submits document and RWI ingest
batches. A shared page-worker pool runs up to the node's configured fetch-worker
limit across all active runs; the active-run limit is separate. Each URL attempt
has a five-second wall-clock deadline. The previous crawler checkpoint is
neither opened nor migrated.

Robots requests are bounded to two seconds, shared across concurrent pages for
the same origin, and cached for up to 24 hours. An unreachable robots file
denies that origin for the cache period, following RFC 9309's fail-closed rule.

Each finished-order log includes request totals, frontier admission totals,
filtered seed totals, page results, and elapsed time. Page workers never wait
for a node ingest acknowledgment: each batch is persisted in the crawler's
outbox and submitted over a bidirectional stream. The stream keeps up to 64
batch submissions in flight and receives acknowledgments independently, letting
the node coalesce page batches. The crawl order stays
leased until those batches are acknowledged. Heartbeats report active URLs,
stages, elapsed time, and up to four next waiting URLs per active run. The Admin
Crawl page shows those URLs and their queued, retry-delay, or host-politeness
state. Slow or failed page-fetch
logs separate host-admission wait and HTTP request time. Slow page-processing
logs identify robots checks, HTML parsing, and outbox persistence. Page starts
have no fleet-wide rate governor; robots rules, per-host politeness, worker
concurrency, and node ingest backpressure still apply.

The replacement crawler requires a node that supports the bidirectional ingest
stream. It does not fall back to unary ingestion.

The current replacement has an HTTP fetch path only. Browser rendering,
pause/resume/cancel/restart controls, per-run pace overrides, filesystem-pressure
admission, and a crawler-local metrics listener are not implemented in this
module yet. Node-reported crawl progress remains available through the existing
admin console.
