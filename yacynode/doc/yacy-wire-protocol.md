# YaCy DHT wire protocol

The peer-to-peer wire protocol is defined in code, and the code is its source of
truth. `/yacy/*` endpoints use plain HTTP form fields. Most peer responses are
`key=value` lines; compatibility endpoints may return YaCy seed text, JSON, or
XML when original YaCy uses those formats.

The bootstrap seed-list endpoints are exceptions: `/yacy/seedlist.html`
returns plain YaCy seed lines, one per CRLF-terminated line, while
`/yacy/seedlist.json` and `/yacy/seedlist.xml` return clear-text peer maps.
The shared blacklist endpoint `/yacy/list.html` is also raw text and returns
CRLF-terminated blacklist entries for `col=black`. The peer profile endpoint
`/yacy/profile.html` is raw text as well and returns CRLF-terminated `key=value`
profile properties. The host-link endpoint `/yacy/idx.json?object=host` returns
JSON with version, uptime, host row definition, and the known host-link index.
The peer message endpoint `/yacy/message.html` supports permission checks and
stores inbound peer messages; attachments are advertised as size `0`. These
match the public P2P surfaces exposed by original YaCy peers.

The crawl URL feed endpoint `/yacy/urls.xml` returns YaCy's RSS-like XML shape.
`call=remotecrawl` currently returns `ok` with no items because remote crawl
delegation is disabled by default. `/yacy/crawlReceipt.html` accepts the YaCy
wire shape and returns no scheduled delay while remote crawl is disabled.
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
