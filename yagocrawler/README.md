# yagocrawler

> **Experimental prototype.** Not production-ready. Interfaces, message shapes, and
> behavior change without notice, and nothing here is stable to build on yet.

An optional, disposable crawl service that fetches URLs, builds extracted
document ingest payloads plus YaCy-compatible RWI postings and URL metadata, and
publishes them toward `yago-node` without storing unbounded raw HTML bodies.

## Why two separate services

`yago-node` is built to run unattended on Raspberry-Pi-class hardware: it stores
and serves YaCy-compatible P2P state and local search building blocks, and
deliberately does not crawl. Crawling is bursty, CPU- and bandwidth-hungry, and
benefits from a real browser engine — work that does not belong on the always-on
node.

So crawling lives here, as a **separate, optional, disposable** service meant to run on
a more powerful machine (a home PC you can freely turn off). It contributes
bounded extracted content for local search plus exactly what the YaCy DHT
natively exchanges: word-index postings and URL metadata. Raw HTML bodies are
not stored or shipped by default.

```mermaid
flowchart LR
    node["yago-node<br/>(Pi-class, always-on)"]
    crawler["yagocrawler<br/>(powerful host, on-demand)"]
    node -- crawl orders --> crawler
    crawler -- "ingest batches (documents + references)" --> node
```

## How it runs

The crawler is a long-running, order-driven service. It dials the node's crawl gRPC
endpoint on startup and then idles until work arrives: the node streams crawl orders
to the crawler, the crawler fetches pages, builds document/RWI/URL metadata
artifacts, and submits ingest batches back to the node over the same connection.
Multiple crawler instances can each stream orders from the node to load-balance,
and the node's blocking ingest call applies backpressure when it
falls behind. Each order is leased rather than handed off: the crawler acks a
finished order, naks a cancelled one back to the node for redelivery, and
heartbeats its in-flight leases, so an order held by a worker that crashes or
disconnects is reclaimed by the node and retried on another worker. Because an
order can therefore be delivered more than once, ingest is idempotent per URL. Document ingest includes the fetched URL and any resolved
`rel=canonical` URL found in the page, plus page-provided description
metadata and bounded image URL/alt metadata when available. Links marked
`rel=nofollow` are not submitted for frontier expansion or local outlink
evidence unless the crawl profile opts in.

Crawl requests can start from normal URLs, XML sitemaps, sitemap indexes, plain
text sitelists, or a host's `robots.txt`. Sitemap and sitelist starts are fetched
through the same proxied public-web egress path as page fetches, parsed before
frontier admission, and expanded into bounded URL roots. A `robots` start fetches
the seed host's `robots.txt` over that same path and expands the sitemaps named in
its `Sitemap:` directives; a missing or unreadable `robots.txt` discovers nothing
rather than failing the crawl. Sitemap `lastmod` values are carried as crawl
request hints for later recrawl scheduling.

Configuration comes from the environment (`YACYCRAWLER_NODE_RPC_ADDR` is required;
`YACYCRAWLER_ALLOW_PRIVATE_NETWORKS` opts into all LAN and private-network targets,
while `YACYCRAWLER_ALLOW_CIDRS` is a comma-separated list of private CIDRs to admit
instead of opening all private space; loopback, link-local, and reserved ranges
stay blocked either way). The service runs until it receives `SIGINT` or
`SIGTERM`, then shuts down gracefully: it stops pulling new jobs but lets
in-flight page fetches finish, waiting up to `YACYCRAWLER_SHUTDOWN_GRACE`
(default `10s`) before aborting any still running.
Outbound fetches, including the headless browser, are screened in-process at dial
time against the connected IP address, so no external forward proxy is required;
the browser routes through a loopback-bound guarded proxy that resolves and dials
targets under the same policy. Before robots.txt or browser navigation starts, the
crawler also rejects non-HTTP(S), loopback, private, link-local, multicast,
unspecified, documentation/test, and metadata-local destinations. The final
rendered URL is checked against the same public-web policy.
The default fetch path uses a bounded HTTP GET first and falls back to the
headless browser only when that fast path rejects the page. The HTTP fast path
follows at most `YACYCRAWLER_MAX_REDIRECTS` redirect hops and uses explicit
request, connect, TLS, and response-header timeout budgets. Sitemap and
sitelist expansion imports at most `YACYCRAWLER_SITEMAP_URL_LIMIT` URLs per
seed. The container image
embeds the pinned headless-shell runtime in a scratch non-root image.

When `YACYCRAWLER_METRICS_ADDR` is set (for example `:9101`), the crawler serves
Prometheus metrics at `/metrics` on that address: `yacy_crawler_jobs_active`,
`yacy_crawler_fetches_total`, `yacy_crawler_fetch_failures_total`,
`yacy_crawler_bytes_total`, `yacy_crawler_robots_denied_total`, and
`yacy_crawler_ingest_batches_total`. When the variable is empty the crawler starts
no metrics server and opens no port.

The message types both services exchange live in the standalone
[`yagocrawlcontract`](../yagocrawlcontract/README.md) module, so neither service depends
on the other.

## Known gaps

- The persistent frontier, politeness model, and recrawl scheduler are still
  prototype-grade.
- Feeding sitemap `lastmod` into persistent frontier recrawl scheduling is still
  planned; discovery from explicit sitemap or sitelist starts and from `robots.txt`
  `Sitemap:` directives (the `robots` start mode) is implemented.
- Browser-level redirect interception is still planned; the current public-web
  admission check, HTTP redirect cap, HTTP timeout budgets, and HTTP final-URL check are
  application-layer guards plus proxy defense in depth.
- Bot-wall handling remains a minimal heuristic, not hardened production
  behavior.
