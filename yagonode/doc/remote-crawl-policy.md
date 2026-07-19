# Remote Crawl Policy

Remote crawl is a YaCy compatibility surface that lets a node delegate bounded
single-URL fetches to explicitly trusted peers. It is disabled by default on
every deployment target.

## Disabled behavior

`/yacy/urls.xml?call=remotecrawl` authenticates the request and returns the
YaCy-compatible feed shape without work. `/yacy/crawlReceipt.html` accepts the
wire shape but returns delay `3600`; it cannot create, continue, or acknowledge
work. `/yacy/urls.xml?call=urlhashlist` remains a read of already stored URL
metadata and never fetches a requested URL.

The node does not advertise the YaCy remote-crawl seed capability flag while
delegation is disabled or incompletely configured.

## Enabling policy

The operator must configure every prerequisite before setting
`YAGO_REMOTE_CRAWL_ENABLED=true` or its Admin equivalent:

- YaCy network authentication mode `salted-magic-sim` with a nonempty shared
  secret;
- 1–256 exact trusted 12-character peer hashes;
- 1–256 positive destination entries, each an exact domain or IP prefix;
  address-family wildcard prefixes are rejected.

An unsafe enable, removal of a required allowlist, or authentication downgrade
is rejected by configuration validation. All controls are available under Admin
Configuration → Swarm and have matching environment bootstrap defaults.

## Work lifecycle

The node observes locally accepted URL crawl orders and asynchronously copies
eligible URLs into a separate durable queue. This optional copy never delays,
rejects, cancels, or removes the authoritative local order. The local crawler
and a trusted peer can therefore fetch the same URL; storage reconciliation and
content deduplication handle that intentional duplication.

A trusted peer obtains at most 100 single-URL items per feed request. The
request budget is clamped to 1–20 seconds. Durable per-peer request windows,
outstanding-lease limits, lease expiry, and queue capacity bound retained work.
Expired leases return to pending state after restart as well as during normal
operation.

Pending selection reads at most 100 durable sequence entries per feed
preparation instead of rescanning the order collection. A versioned startup
reconciliation rebuilds missing pending, expiry, and peer-lease state from
authoritative order rows in batches; normal state transitions update those
records in the same transaction.

Receipts must authenticate as the leasing peer and carry the exact URL hash and
canonical URL from an unexpired lease. Metadata is decoded within a 256 KiB
limit and its URL within an 8 KiB limit. Only `fill` stores the matching URL
metadata and completes the lease. The YaCy outcomes `unavailable`, `exception`,
`robot`, `rejected`, `dequeue`, `update`, `known`, and `stale` return delay `3600`
and requeue the work. A policy rejection returns delay `9999`; an accepted fill
returns delay `10`. Replayed, mismatched, expired, and malformed receipts cannot
create, replace, or extend work.

## Destination safety

Only hierarchical HTTP and HTTPS URLs on their default ports are eligible.
Credentials are rejected and fragments are removed. Exact domain or IP-prefix
admission and every DNS answer are checked when the order is staged, before it
is leased, and before a receipt commits. Loopback, link-local, multicast,
unspecified, cloud metadata, and reserved destinations remain denied. Private
addresses require an explicit matching IP prefix; a domain entry alone does not
admit a private DNS answer.

Remote delegation transfers a URL and bounded metadata, not response bodies,
crawl profiles, redirects, or follow-up depth. A trusted peer cannot use a
receipt to request another fetch.

## Operations

Defaults are 60 feed requests per peer per minute, 10 outstanding URLs per peer,
a 10-minute lease, and 1,000 queued URLs. The maximums are 10,000 requests per
minute, 100 outstanding URLs, a 24-hour lease, and 100,000 queued URLs.

The node records every decision with the stable `remote crawl decision` log
message and `remote_crawl_decisions_total{action,outcome}`. Only warning and
security outcomes enter the durable Admin event history; accepted leases,
accepted receipts, and ordinary requeues remain in debug logs and metrics. Queue
saturation and delegation errors leave local crawl work intact.
