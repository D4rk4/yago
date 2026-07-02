# 12. Use Bleve for the embedded full-text fallback

Date: 2026-07-02

## Status

Accepted

## Context

`yago-node` must not use YaCy RWI as the primary local full-text search engine.
The node still needs a simple pure-Go profile that can run without a JVM, Solr,
Lucene, Kelondro, or a Rust sidecar. The production target remains a Tantivy
sidecar, but the first local backend should give the Go node BM25 lexical search
over stored documents while keeping RWI as the YaCy P2P exchange layer.

The fallback backend must run on small Linux hosts, stay portable in the current
CGO-free build, support document title and body fields, allow an index rebuild
from the document store, and avoid becoming a compatibility claim for Java YaCy
ranking parity.

## Decision

Use `github.com/blevesearch/bleve/v2` as the first embedded full-text fallback
backend.

The initial adapter is in-memory and rebuilt from the document store on node
startup. Crawler ingest updates the document store first and then updates the
full-text index before URL metadata and RWI postings are acknowledged. Local
public search uses this `SearchIndex` path, while `/yacy/search.html`,
`/yacy/transferRWI.html`, and `/yacy/transferURL.html` keep using the YaCy RWI
and URL metadata compatibility stores.

## Considered alternatives

Tantivy was considered because it is the preferred production-quality backend in
the roadmap. It was not chosen for this fallback because the project needs a
bounded sidecar protocol and operational policy before adding a Rust runtime
boundary.

Bluge was considered because it supports BM25 and highlighting in pure Go. It
was not chosen first because Bleve has broader documentation, a larger ecosystem,
and a clearer fit for fielded document indexing in the current Go codebase.

SQLite FTS5 was considered because it is mature and inspectable. It was rejected
for this fallback because it introduces CGO or alternate SQLite driver tradeoffs
before the node needs SQL-backed search storage.

Keeping RWI-only local search was considered. It was rejected because RWI is a
compatibility and exchange layer, not the primary local search engine.

## Consequences

Bleve v2 becomes a runtime dependency of `yacynode` and is pinned in
`yacynode/go.mod`. The first adapter does not persist its own index; it rebuilds
from bounded stored documents on startup. That keeps backup and migration policy
simple while the project validates search behavior and later adds the Tantivy
sidecar or a persistent embedded profile.

Memory usage now depends on the number and size of stored document fields that
are indexed. The existing document-store size limit and storage quota remain the
primary guardrails. Large deployments should move to the Tantivy sidecar or a
persistent indexed backend before increasing crawl volume.
