# Node ↔ crawler communication contract

This describes the messages a YaCy RWI node and a disposable crawl service exchange.
The types live in the `yacycrawlcontract` module so neither service depends on the other.

For why crawling is a separate service, see the [crawler README](../yacycrawler/README.md).

## Two one-way topics

The seam has exactly two topics, each flowing one direction:

```
            CrawlOrder  (work down)
  ┌───────┐ ───────────────────────▶ ┌───────────┐
  │ node  │                          │  crawler  │
  └───────┘ ◀─────────────────────── └───────────┘
            IngestBatch (results up)
```

Keeping every topic one-way is what lets many crawlers fan in to one node without any
addressing: a result on a shared topic is consumed by the node, never routed back to a
specific crawler instance.

## Down: `CrawlOrder`

A crawl order carries one ruleset (`CrawlProfile`) and the seed URLs that reference it.
Both ways of starting a crawl — an operator starting one locally, or another peer
ordering remote crawl work — produce the same order shape, so the crawler never learns
who asked.

- `Provenance` is an opaque, node-owned token. The crawler never inspects it; it only
  echoes it back on the results so the node can attribute them. Admin vs. remote lives
  entirely inside this token.
- `Profile` travels inline. The crawler keeps an in-memory registry keyed by the
  profile handle so links discovered mid-crawl resolve their ruleset. Nothing is stored;
  if the node restarts the crawler, it re-sends orders.

### Backpressure, not acknowledgement

There is no per-order acknowledgement. The order topic is a bounded queue: when the
crawler is saturated, the node's publish blocks. Whether to accept more work is a
node-side decision made before publishing, so it needs no confirmation from the crawler.

## Up: `IngestBatch`

Each batch carries the postings and URL metadata for one fetched page, plus the
originating order's `Provenance` and `ProfileHandle` so the node can attribute it.

There is no per-batch feedback topic. A reply on a shared topic could not be routed back
to the batch's sender under multiple crawlers, backpressure is already structural via the
bounded ingest queue, and there is no two-phase URL reconciliation to do because a batch
ships postings and metadata together.

## Crawl parameters

A references-only crawler inherits the subset of YaCy's crawl profile that affects which
URLs are fetched and how references are produced. The rest is deferred.

### Inherited

| Profile field | Meaning |
|---|---|
| `Name`, `Handle` | profile label and its 12-char identity hash |
| `Scope` | wide / domain / subpath link-following bound |
| `URLMustMatch` | regex a URL must match to be followed (default matches all) |
| `URLMustNotMatch` | regex a URL must not match (default empty) |
| `MaxDepth` | maximum link-following depth |
| `AllowQueryURLs` | whether to follow URLs bearing a query string |
| `MaxPagesPerHost` | per-host page cap (-1 = unlimited) |
| `RecrawlIfOlder` | re-crawl age threshold (0 = never recrawl) |
| `CrawlDelay` | politeness delay between requests |

Per-URL, a `CrawlRequest` carries the URL, referrer, anchor text, depth, the profile
handle it follows, the initiating peer (empty for a local crawl), and the appearance date.

The handle is the first 12 characters of a hash over the ruleset-defining fields
(`Name`, `URLMustMatch`, `MaxDepth`, `URLMustNotMatch`, `MaxPagesPerHost`), so identical
rulesets collapse to one handle.

### Deferred

The User-Agent is **not** a profile field. The crawler drives a real browser that fixes
its User-Agent once at startup, process-wide; an order cannot override it without breaking
the browser's fingerprint consistency. It stays a crawler-process setting.

Also deferred: text/media index toggles (this crawler only ever indexes text references),
all index-time content and media-type filters, snapshots, vocabulary scraping, IP and
country filters, the HTTP cache, robots `noindex`/`nofollow` handling (a likely next
addition), the direct/NOLOAD document path, and re-distributing crawl orders onward (this
*is* the remote crawler).
