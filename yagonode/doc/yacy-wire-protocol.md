# YaCy DHT wire protocol

The peer-to-peer wire protocol is defined in code, and the code is its source of
truth. `/yacy/*` endpoints use plain HTTP form fields. Most peer responses are
`key=value` lines; compatibility endpoints may return YaCy seed text, JSON, or
XML when original YaCy uses those formats.

Every authenticated YaCy request adapter retains whether `network.unit.name`
was absent or explicitly empty. An absent field defaults to `freeworld`; a
present empty value is not rewritten and therefore fails authentication
against `freeworld` in both open and salted modes. An empty local network
configuration still means `freeworld`.

The hello request preserves `iam` as an opaque form value for exact salted
authentication. Its optional `count` uses Java signed-decimal int32 parsing:
BMP decimal digits have their `Character.digit(char, 10)` values, only ASCII
`+` and `-` signs are accepted, and supplementary digits are invalid. The value
falls back to zero when absent, malformed, or outside the signed int32 range.
The raw hello `seed` field is accepted through 16,000 Java `String.length()`
UTF-16 units and rejected above that boundary before generic seed decoding.

The bootstrap seed-list endpoints are exceptions: `/yacy/seedlist.html`
returns plain YaCy seed lines, one per CRLF-terminated line, while
`/yacy/seedlist.json` and `/yacy/seedlist.xml` return clear-text peer maps.
The seed-list request supports YaCy filters for count, peer type, self
inclusion, own seed selection, peer hash, peer name, address-only output,
JSONP callback, and minimum peer version.
`maxcount` uses the same Java signed-decimal int32 parser as hello `count`.
Seed import accepts the documented signed `UTC` offset form and the timestamp
form observed in current freeworld seedlists, preserving whichever value the
remote seed carries.
Bare, plain (`p|`), base64 (`b|`), and gzip (`z|`) wire forms are checked against
the receiving field's decoded-size budget before parsing. The compatibility
default is 4 MiB. A compact seed is limited to 32 KiB, 128 properties,
128-byte keys, 8 KiB generic or news values, and a 256-byte peer name;
bootstrap retains at most 4,096 seeds/16 MiB and peer selection reuses a
4,096-peer/16 MiB owned snapshot.
The shared blacklist endpoint `/yacy/list.html` is also raw text. It checks the
YaCy network unit and returns the `col=black` list files named by
`YAGO_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`, read from
`YAGO_DATA_DIR/LISTS`. The peer profile endpoint `/yacy/profile.html` is raw text
as well and returns CRLF-terminated `key=value` profile properties loaded from
`YAGO_DATA_DIR/SETTINGS/profile.txt` when present. Both routes admit four
requests before form parsing. Blacklist config, names, files, and response share
a 16 MiB budget. Profile input is capped at 1 MiB and 1,024 properties/1 MiB of
owned property data; its encoded response is capped at 2 MiB. Either route
returns its compatible empty response instead of partial data on overflow.
The host-link endpoint `/yacy/idx.json?object=host` returns
JSON with version, uptime, YaCy's host-reference row definition, and a bounded
incoming host-link index inferred from stored document outlinks. Collection
is capped at 4,096 target hosts, 64 source hosts per target, and 32,768 total
references before graph entries are allocated. The completion-relative
background corpus pass builds the graph with the other corpus signals, stores
it in the same atomic checkpoint, and publishes an immutable snapshot before
listeners open. Four endpoint requests are admitted concurrently; they only
read the snapshot and never start or wait for a document-store scan.
The peer message endpoint `/yacy/message.html` supports permission checks and
stores inbound peer messages; `iam` is optional, permission checks ignore
post-only body fields, and attachments are advertised as size `0`. These match
the public P2P surfaces exposed by original YaCy peers.

The remote search endpoint `/yacy/search.html` returns YaCy key-value rows with
`searchtime`, `references`, `joincount`, `count`, `resourceN`, `indexcount.*`,
and `indexabstract.*` fields. `count` is the number of `resourceN` rows carried
in the response.

The peer query endpoint `/yacy/query.html` requires the called peer hash in
`youare`. It answers `rwicount`, `rwiurlcount` with `env=<wordhash>`,
`lurlcount`, and the upstream `wanted*` probe names with numeric `response`
values.

The public search endpoints `/yacysearch.json`, `/yacysearch.rss`, and
`/yacysearch.html` share the same request parsing and search backend.
`/yacysearch.json` returns JSON with `channels`, `items`, `navigation`, and
`totalResults` fields following the original YaCy template shape.
`/yacysearch.rss` returns OpenSearch-flavored RSS 2.0 with YaCy item metadata.
`/yacysearch.html` returns a simple HTML search form and result list. Local
results come from the document-backed full-text `SearchIndex` path.
`/opensearchdescription.xml` advertises the HTML, RSS, JSON suggestion, and XML
suggestion URLs for the current public base URL. `/suggest.json` returns the
OpenSearch suggestions JSON array shape from bounded in-memory recent queries.
`/suggest.xml` returns the YaCy-compatible `SearchSuggestion` XML shape with
the same suggestion source.

For public search, `resource=local` searches the local full-text index over
stored documents. `resource=global` searches the local node and performs bounded YaCy
`/yacy/search.html` fanout to reachable peers selected by the query term hashes'
YaCy DHT positions, configured redundancy, and configured vertical partition
exponent. Every outbound search carries the node's current compact seed in
`myseed`, which current YaCy peers require before they execute the query.
Multi-term remote searches first request YaCy `indexabstract` rows for
each term, intersect the returned URL hashes locally, and then perform bounded
secondary `urls=` retrieval for the intersected hashes. Peers must have a
reachable address, advertise remote-index intake, pass the upstream age gate,
and advertise non-empty RWI inventory before they are eligible remote search
targets. When more redundant DHT candidates are eligible than the query fanout
will use, the node samples the target set randomly to spread repeated searches
across compatible peers.
Remote peer failures, malformed responses, missing reachable peers, and missing
DHT targets are returned as partial-failure metadata instead of turning the
whole public search request into an HTTP error.

The crawl URL feed endpoint `/yacy/urls.xml` returns YaCy's RSS-like XML shape.
`call=remotecrawl` returns `ok` with no items by default. An explicitly enabled
controlled-network policy requires `salted-magic-sim`, a shared secret, exact
trusted peer hashes, and exact domain or IP-prefix destinations. It leases at
most 100 durable single-URL items to the authenticated trusted `iam`, inside the
request's clamped 1–20 second budget. Per-peer request windows, outstanding
leases, expiry, and pending requeue survive restart.

`/yacy/crawlReceipt.html` accepts the bounded YaCy wire shape. Disabled,
authentication, malformed, mismatched, expired, and valid non-fill outcomes
return delay `3600`; destination-policy rejection returns `9999`; an unexpired
peer-and-URL-matching `fill` stores the bounded URL metadata, removes the lease,
and returns `10`. The accepted result vocabulary is `unavailable`, `exception`,
`robot`, `rejected`, `dequeue`, `fill`, `update`, `known`, and `stale`. A receipt
cannot create or extend work.
`call=urlhashlist` accepts concatenated 12-byte URL hashes and returns the
locally stored metadata rows it knows. The execution policy is documented in
[Remote Crawl Policy](remote-crawl-policy.md).

Two modules hold the definitions, each browsable with `go doc`:

- `yagomodel` — the value-level types and their codecs: enhanced Base64, hashes,
  DHT ring positions, peer types, seeds and their wire forms, RWI postings, and
  URL metadata rows.
- `yagoproto` — the per-endpoint request and response data transfer objects, the
  endpoint paths, the wire field names, and network authentication, all built on
  `yagomodel`.

```sh
go doc ./yagomodel
go doc ./yagoproto
```

For the reasoning behind the two-module split, see
[ADR 0004](adr/0004-isolate-wire-protocol-module.md).

Golden peer-wire fixtures live in `yagonode/test/fixtures/yacywire/`. They keep
plain request and response forms for `hello.html`, `query.html`,
`transferRWI.html`, `transferURL.html`, and `search.html`, and tests parse and
encode them through `yagoproto` so future protocol changes are checked against
stable YaCy wire shapes.

`/yacy/transferRWI.html` preserves upstream preflight result strings for network
authentication failure and missing required fields before accepting RWI rows.
`/yacy/transferURL.html` preserves upstream network-auth failure behavior by
returning no transferURL result fields before target handling.
