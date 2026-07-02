# YaCy DHT wire protocol

The peer-to-peer wire protocol is defined in code, and the code is its source of
truth. `/yacy/*` endpoints use plain HTTP form fields. Most peer responses are
`key=value` lines; compatibility endpoints may return YaCy seed text, JSON, or
XML when original YaCy uses those formats.

The bootstrap seed-list endpoints are exceptions: `/yacy/seedlist.html`
returns plain YaCy seed lines, one per CRLF-terminated line, while
`/yacy/seedlist.json` and `/yacy/seedlist.xml` return clear-text peer maps.
The seed-list request supports YaCy filters for count, peer type, self
inclusion, own seed selection, peer hash, peer name, address-only output,
JSONP callback, and minimum peer version.
Seed import accepts the documented signed `UTC` offset form and the timestamp
form observed in current freeworld seedlists, preserving whichever value the
remote seed carries.
The shared blacklist endpoint `/yacy/list.html` is also raw text and returns
the `col=black` list files named by `YACY_DATA_DIR/SETTINGS/yacy.conf`
`BlackLists.Shared`, read from `YACY_DATA_DIR/LISTS`. The peer profile endpoint
`/yacy/profile.html` is raw text as well and returns CRLF-terminated `key=value`
profile properties loaded from `YACY_DATA_DIR/SETTINGS/profile.txt` when present.
The host-link endpoint `/yacy/idx.json?object=host` returns
JSON with version, uptime, YaCy's host-reference row definition, and a bounded
incoming host-link index inferred from stored URL metadata referrers.
The peer message endpoint `/yacy/message.html` supports permission checks and
stores inbound peer messages; permission checks ignore post-only body fields and
attachments are advertised as size `0`. These match the public P2P surfaces
exposed by original YaCy peers.

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
`/yacysearch.html` returns a simple HTML search form and result list.
`/opensearchdescription.xml` advertises the HTML, RSS, JSON suggestion, and XML
suggestion URLs for the current public base URL. `/suggest.json` returns the
OpenSearch suggestions JSON array shape from bounded in-memory recent queries.
`/suggest.xml` returns the YaCy-compatible `SearchSuggestion` XML shape with
the same suggestion source.

For public search, `resource=local` searches local RWI and URL metadata only.
`resource=global` searches the local node and performs bounded YaCy
`/yacy/search.html` fanout to reachable peers selected by the query term hashes'
YaCy DHT positions, configured redundancy, and configured vertical partition
exponent. Multi-term remote searches first request YaCy `indexabstract` rows for
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
`call=remotecrawl` currently returns `ok` with no items because remote crawl
delegation is disabled by default. `/yacy/crawlReceipt.html` accepts the YaCy
wire shape, checks the target peer hash, and returns the YaCy rejected-receipt
retry delay while remote crawl is disabled.
`call=urlhashlist` accepts concatenated 12-byte URL hashes and returns the
locally stored metadata rows it knows. The execution policy is documented in
[Remote Crawl Policy](remote-crawl-policy.md).

Two modules hold the definitions, each browsable with `go doc`:

- `yacymodel` — the value-level types and their codecs: enhanced Base64, hashes,
  DHT ring positions, peer types, seeds and their wire forms, RWI postings, and
  URL metadata rows.
- `yacyproto` — the per-endpoint request and response data transfer objects, the
  endpoint paths, the wire field names, and network authentication, all built on
  `yacymodel`.

```sh
go doc ./yacymodel
go doc ./yacyproto
```

For the reasoning behind the two-module split, see
[ADR 0004](adr/0004-isolate-wire-protocol-module.md).

Golden peer-wire fixtures live in `yacynode/test/fixtures/yacywire/`. They keep
plain request and response forms for `hello.html`, `query.html`,
`transferRWI.html`, `transferURL.html`, and `search.html`, and tests parse and
encode them through `yacyproto` so future protocol changes are checked against
stable YaCy wire shapes.
