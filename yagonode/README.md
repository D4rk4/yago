# YaGo node

The `yago-node` process serves the local search index, YaCy-compatible peer
endpoints, and the public search APIs.

## Progressive local search

Eligible first-page plain queries return a useful randomized shard sample as
soon as one is available. Responses identify sampled and fully searched shards
so clients can distinguish partial coverage from a complete empty result. A
bounded full-shard search then continues for that query. Only complete results
are retained in the existing five-minute, 32 MiB in-memory session cache; a
process restart clears those cached responses.

Browser clients can consume updates through the YaCy-compatible
`/yacysearch.ndjson` endpoint. The HTML search page updates as those results
arrive. Tavily-compatible `/search` remains a single-response endpoint with
the documented Tavily shape. Exact and later-page searches retain their
existing behavior.

The randomized shard position is an efficiency technique, not a statistical
guarantee of uniform sampling over matching documents. See
[`doc/adr/0091-randomize-deadline-bounded-shard-retrieval.md`](doc/adr/0091-randomize-deadline-bounded-shard-retrieval.md)
and
[`doc/adr/0086-return-useful-shard-samples-and-cache-complete-searches.md`](doc/adr/0086-return-useful-shard-samples-and-cache-complete-searches.md)
for the design and resource bounds.

The authenticated Admin console is available at `/admin/` on the ops listener.
The Performance page shows per-process fetched and indexed rates, current worker
activity, fetch-admission waits, queued orders, and evidence-based signals.
Rates include pages completed by short crawl runs during the 60-second window.
The Crawler page shows the latest reported pending URL total across active runs
separately from node orders waiting for or leased to crawlers. It lists active
crawls with their worker report age, up to four URLs from the oldest pending
node orders, and each worker's current and next URL sample with readable stages
and wait reasons. It keeps the ten most recent terminal runs under a collapsed
history section.
The node keeps crawl orders in `${YAGO_DATA_DIR}/crawlbroker.db`. Its soft
ceiling defaults to `auto`: 10% of the effective storage quota, capped by free
space after the configured node reserve; with unlimited quota it uses 10% of
measured free space after reserve. A byte size in `YAGO_CRAWLER_NODE_STATE_MAX_BYTES`
or Admin setting `crawler.node_state_max_bytes` selects a fixed ceiling, while
zero disables it.
Through a public HTTPS hostname, route `/admin/*` and `/api/admin/*` to the ops
listener and keep that listener private; configure the reverse proxy's address
in `YAGO_TRUSTED_PROXIES` so login cookies receive the Secure attribute.

Run the browser scenarios from [`ui-tests`](ui-tests/README.md).
