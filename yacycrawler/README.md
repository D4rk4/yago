# yacycrawler

> **Experimental prototype.** Not production-ready. Interfaces, message shapes, and
> behavior change without notice, and nothing here is stable to build on yet.

An optional, disposable crawl service that fetches URLs, builds YaCy-compatible RWI
postings and URL metadata, and publishes them toward a YaCy RWI node without storing
document bodies.

For what the package does and how the pieces fit together, see the package doc in
[`doc.go`](doc.go).

## Known gaps

- The node ingest side is faked; there is no real broker or real node integration.
- URL hashing is not yet verified against the YaCy Java reference.
- Politeness and bot-wall handling are minimal heuristics, not hardened.
