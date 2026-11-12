# YaGo node

The `yago-node` process serves the local search index, YaCy-compatible peer
endpoints, and the public search APIs.

When the public search portal is enabled, `/api` provides an agent API
quickstart with authentication and a search example. It links to the full
Tavily-compatible request and response reference in `doc/compatibility.md`.

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
[`doc/adr/0085-randomize-deadline-bounded-shard-retrieval.md`](doc/adr/0085-randomize-deadline-bounded-shard-retrieval.md)
and
[`doc/adr/0086-return-useful-shard-samples-and-cache-complete-searches.md`](doc/adr/0086-return-useful-shard-samples-and-cache-complete-searches.md)
for the design and resource bounds.

On graceful shutdown, the node joins its storage-using background workers before
closing the vault and checkpoints each shard's freelist. A subsequent clean
startup can load those freelists directly; a crash or skipped checkpoint can
require a full shard scan.

An existing Bleve index opens without a full read-cache scan or forced segment
merge. After the node binds its listeners, those read-preparation tasks run in
the background while searches read required pages on demand.

An administrator can also save the shard freelists from Configuration → Storage
while the node remains running. Vault reads and writes pause until all shards
finish; the node then resumes with freelist persistence enabled for later writes.

The authenticated Admin console is available at `/admin/` on the ops listener.
The Performance page shows per-process fetched and indexed rates, current worker
activity, fetch-admission waits, the crawler's reported pending frontier, and
evidence-based signals. Rates come from cumulative crawler progress reports.
Authenticated `/metrics` also reports per-stage crawl-ingest latency, including
content-cluster replacement and the refreshes it applies to existing cluster
members, so queue wait, storage, search indexing, URL metadata, and posting
writes can be compared directly. Transition-commit timing covers grouped
reconciliation after replay evidence is durable. Vault update phases report read
yield, writer admission, global-gate wait, transaction work, shard transaction
opening, commit validation, each touched-shard commit, and total commit time.
These nested samples use fixed phase and attempt labels without shard or URL
identifiers.
The Crawler page accepts focused URL and topic demand and displays the crawler's
reported pending total, current URL, waiting URL samples, report age, and fetched
page outcomes. The outcome view separates document candidates, accepted and
rejected mutations, pages fetched without a document mutation, pending ingest,
and older pages whose outcomes were not recorded. The node
does not keep a crawl-order queue or decide which URLs to fetch. It records search
demand and accepts authenticated document mutations; `yago-crawler` owns URL
selection, retries, recrawl timing, robots policy, and host politeness.
Through a public HTTPS hostname, route `/admin/*` and `/api/admin/*` to the ops
listener and keep that listener private; configure the reverse proxy's address
in `YAGO_TRUSTED_PROXIES` so login cookies receive the Secure attribute.

Run the browser scenarios from [`ui-tests`](ui-tests/README.md).
