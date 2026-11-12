# yago-crawler

`yago-crawler` is the independent worker that chooses and fetches pages. It
stores one persistent URL frontier under
`${YAGO_DATA_DIR}/yago-crawler/frontier.db`. It reads incremental search-demand
records from `yago-node`, ranks active topics and URLs, follows admitted links,
and owns retries and recrawl timing. The node does not queue crawl orders.
An explicit focus makes a completed URL eligible again when its last completion
predates the focus; a later completion prevents an unnecessary repeat fetch.
If the node's demand sequence is restored behind the worker's saved cursor, the
worker resets that cursor and resumes from the available demand records.

The HTTP worker checks robots rules, applies host politeness and the shared URL
denylist, and gives each URL attempt at most five seconds before settling or
retrying it. It stores fetched document mutations in a durable outbox before
sending them asynchronously to the node. A successful mutation updates the
document, full-text, URL-metadata, and RWI stores. Permanent 404 and 410 results
remove stale indexed documents.

The frontier atomically grants at most one active page to a hostname across
overlapping crawl runs. Other workers skip that hostname and continue with an
eligible host. A 429 response applies a persistent host-wide cooldown using the
server's `Retry-After` value or bounded exponential delay; queued pages for that
host remain deferred while other hosts continue. Successful fetches clear the
consecutive rate-limit streak. Per-host politeness controls the minimum time
between starts.

The worker reports its pending frontier size, active fetches, current URLs, and
sampled waiting URLs through authenticated heartbeats. Progress reports include
cumulative fetched and node-accepted totals, document candidates, removals,
fetches with no document mutation, node rejections, pending outbox records, and
bounded reason counts. The Admin console derives per-minute rates from crawler
reports and shows pages fetched before outcome accounting as unknown. Node order
queue depth is not used as a crawler metric.

During an active frontier run, the worker also writes one structured JSON
progress record per minute. It separates completed fetch and index rates from
HTTP request starts, robots requests, status classes, host-admission waits,
request duration, node ingest sends and acknowledgments, frontier state, and
pending outbox size. Each record includes cumulative candidate, no-mutation, and
node-rejection counts and their bounded reasons. It includes activity for up to eight busy hostnames and
reports when additional hosts were omitted. Slow-stage and failure records use
the persistent page ID and hostname, without URL paths or query text. Routine
stage starts do not add a log line for every page.
The JSON records go to standard error. Read them with
`docker compose logs yago-crawler` under Docker or
`journalctl -u yago-crawler` under systemd.

The current worker uses the HTTP fast path and does not render pages in a
browser. It supports sitemap discovery within the configured depth ceiling,
bounded response sizes, URL canonicalization, retries with backoff, SSRF
protection, and restart recovery from its local frontier and outbox.

## Configuration

Set `YAGO_CRAWLER_NODE_RPC_ADDR` to the node's gRPC listener and
`YAGO_CRAWLER_NODE_API_KEY` to an Admin API key with the `crawl:write` scope.
Set `YAGO_DATA_DIR` to the directory that holds durable crawler state. The
optional `YAGO_CRAWLER_WORKER_ID` distinguishes multiple worker processes.

The operator creates and revokes the API key through the node Admin security
settings. Docker Compose and systemd examples show where to provide it. Do not
put the key in source control or logs.

## Run

Start `yago-node` before `yago-crawler`. The Docker image and systemd unit run
the same `yago-crawler` binary. Restarting the worker preserves its frontier
and pending document outbox.
