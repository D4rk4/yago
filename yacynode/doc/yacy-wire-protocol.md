# YaCy DHT wire protocol

The peer-to-peer wire protocol is defined in code, and the code is its source of
truth. All `/yacy/*` endpoints use plain HTTP: requests are HTTP form fields and
responses are `key=value` lines, with no JSON or XML.

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
