# YaCy Upstream Test Parity

This document maps the upstream YaCy Search Server JUnit suite to equivalent
tests in this repository. The goal is confidence that the P2P swarm behavior of
`yago-node` matches Java YaCy where both implement the same protocol surface.
Upstream tests are used as a source of protocol facts and test vectors; no Java
code is copied. Upstream reference: `yacy/yacy_search_server`, directory
`test/java`.

Statuses:

- `ported`: the upstream assertions exist here as Go tests over this codebase.
- `pre-existing`: this repository already had equivalent or stronger tests
  before this mapping was recorded.
- `deviation`: this implementation intentionally behaves differently; the
  difference and its follow-up are stated.
- `planned`: the upstream test targets functionality this node does not
  implement yet; the port lands with that feature.
- `not applicable`: the upstream test targets Java internals or surfaces this
  node does not reimplement.

## P2P swarm relevant

| Upstream test | Status | Where / why |
| --- | --- | --- |
| `peers/NewsPoolTest` | planned | Peer news exchange is a planned slice; the upstream facts (news categories, per-record distribution counter of 30 before moving to the published queue) drive that implementation. |
| `peers/operation/yacyVersionTest` | ported (filter subset) | `yacynode/internal/seedlist/minversion_yacy_fixture_test.go` ports the combined-version vectors against seedlist `minversion` filtering: valid `d.dddSVVV` values order numerically, malformed values never pass. The upstream `combined2prettyVersion` display split has no Go counterpart because this node never renders pretty version strings. |
| `peers/graphics/WebStructureGraphTest` | deviation | Upstream counts anchor references per document into a host-to-host structure keyed by host hash. This node infers a bounded incoming host graph from stored URL metadata referrers (`yacynode/internal/hostlinks/index_endpoint_test.go`, `yacynode/internal/yagonode/host_link_source_test.go`); it is keyed by the same YaCy host hashes but counts stored referrer URLs, not anchors. Anchor-level structure needs a document outlink store and remains future work. |
| `peers/graphics/ProfilingGraphTest` | not applicable | Server-side UI graphics rendering. |
| `cora/document/id/DigestURLTest` | pre-existing | `yacymodel/url_hash_test.go`: `TestYaCyDigestURLDefaultPortNormalformFixtures` (identPort), `TestYaCyDigestURLHostHashFixtures` (hosthash equality and inequality), `TestYaCyDigestURLFileHashFixtures` (file URL slash variants). |
| `cora/document/id/MultiProtocolURLTest` | pre-existing | `yacymodel/url_hash_test.go`: session-ID removal, backpath, and normalform fixture sets. |
| `cora/document/id/DigestURLHashPerfTest` | not applicable | Performance benchmark, no protocol assertions. |
| `cora/date/GenericFormatterTest` | ported (wire subset) | `yacymodel/seed_time_yacy_fixture_test.go` pins the `yyyyMMddHHmmss` SHORT_SECOND wire form used by seed `BDate`/`LastSeen` with the upstream sample values. The other upstream patterns (SHORT_DAY, SHORT_MINUTE, MILSEC, RFC1123, ANSIC, SIMPLE) do not appear on this node's implemented wire surfaces. |
| `cora/protocol/DomainsTest` | pre-existing (model subset) | Seed addresses arrive as separate `IP`/`Port`/`IP6` DNA attributes handled by `yacymodel/seed_host_test.go`, `seed_port_test.go`, and `seed_address_test.go`; this node has no scheme-stripping helper because it never parses `scheme://host:port/path` peer addresses. |
| `kelondro/data/word/WordReferenceVarsTest` | not applicable | min/max aggregation of Java ranking variables inside `WordReferenceVars`; this node does not reimplement that merge structure. Posting field integrity is covered by the reference-container port below. |
| `kelondro/rwi/ReferenceContainerTest` | ported | `yacynode/internal/rwi/posting_directory_yacy_fixture_test.go`: a posting stored for a word hash is retrievable by URL hash with every ranking property preserved, mirroring the upstream distance-preservation assertion. |
| `search/query/QueryGoalTest` | ported (hash-level) + deviation | `yacymodel/hash_yacy_fixture_test.go` proves case-insensitive word hashing with apostrophes preserved, so the DHT word-hash set matches upstream for plain terms; `yacynode/internal/searchcore/query_yacy_fixture_test.go` pins term boundaries. Deviations: double-quoted phrases are split into single terms instead of kept as one include string, single-quoted phrases are not recognized, and a bare `+` prefix stays part of the term. These land with the SEARCH-02 query parser compatibility slice. |
| `server/serverObjectsTest` | not applicable | Upstream test prints encodings and asserts nothing. |
| `cora/document/feed/RSSFeedTest` | pre-existing (endpoint level) | `/yacy/urls.xml` feeds are covered by `yacynode/internal/crawlurls/*_test.go` golden shapes. |
| `kelondro/data/meta/URIMetadataNodeTest` | not applicable | Icon extraction from Solr collection fields; this node stores favicon and image metadata through the document store instead. |

## Not swarm relevant

Document parsers (`document/*`, `parser/*`), crawler internals
(`crawler/*`), local index internals (`kelondro/index`, `kelondro/io`,
`search/index`, `search/snippet`, `search/ranking`), UI and data tooling
(`data/*`, `http/*`, `utils/*`, `visualization/*`, `Vocabulary_pTest`,
`yacysearchitemTest`, `ImageViewer*`), and Solr connectors
(`cora/federate/solr/*`) test surfaces this node either does not reimplement
or covers through its own module tests.

## Wire surfaces without upstream unit tests

Upstream has no JUnit coverage for the `/yacy/*` servlet wire behavior itself
(hello, query, transferRWI, transferURL, search, seedlists, urls, list,
message, profile, crawlReceipt). This repository covers those surfaces with
golden wire fixtures under `yacynode/test/fixtures/yacywire/` and endpoint
tests in the matching `yacynode/internal/*` packages, derived from upstream
servlet sources and observed responses. Functional end-to-end compatibility
against a live Java YaCy peer is the planned interop matrix in
`yacynode/test/e2e`.
