# 12. Use Bleve for the embedded full-text fallback

Date: 2026-07-02

## Status

Accepted

Amended by [ADR-0018](0018-commit-to-bleve-web-search-backend.md): Bleve is the
committed web-search backend and the Tantivy production sidecar is dropped from the
roadmap.

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

The adapter persists its index under `YAGO_DATA_DIR/search.bleve`. Startup opens
the existing index and rebuilds from the document store only when the index is
missing or unusable. Crawler ingest updates the document store first and then
updates the full-text index before URL metadata and RWI postings are
acknowledged. Local public search uses this `SearchIndex` path, while
`/yacy/search.html`, `/yacy/transferRWI.html`, and `/yacy/transferURL.html` keep
using the YaCy RWI and URL metadata compatibility stores.

When a mapping change requires a rebuild, startup first counts the document
corpus and measures the current index footprint when it exists. It applies the
normal storage-growth admission with that footprint as additional required
headroom before it removes the old index. A rejected preflight preserves both
the old index and the durable rebuild marker. An admitted rebuild reports one
structured start record, at most the nine 10% milestones before completion, and
one completion record while continuing to write 16-document batches.

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

Bleve v2 becomes a runtime dependency of `yagonode` and is pinned in
`yagonode/go.mod`. The embedded fallback now writes a separate persistent search
index next to the node vault. Backup and restore must include both
`yago-node.db` and `search.bleve` when this backend is in use. The document store
remains the source of truth for repair rebuilds and future backend migrations.
Rebuild headroom is advisory rather than an exact future peak: allocation and
segment merging can use more space than the measured current footprint. An
interrupted rebuild remains restart-from-beginning, and its progress records are
operational evidence rather than a resumable checkpoint.

Memory usage now depends on the number and size of stored document fields that
are indexed. Disk usage now includes the Bleve index in addition to the document
store. The existing document-store size limit and storage quota remain the
primary ingest guardrails, but large deployments should apply the Bleve tuning in
[ADR-0018](0018-commit-to-bleve-web-search-backend.md) and keep the working set in
memory before increasing crawl volume.
