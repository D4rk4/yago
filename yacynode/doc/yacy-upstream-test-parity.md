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
| `peers/NewsPoolTest` | ported (pool level) | `yacynode/internal/peernews/news_pool_test.go` ports the publication fixture: three published records are each offered thirty times through the outgoing queue and then rest in the published queue with the full distribution count; the record wire form, id composition (creation second plus originator hash with the `#` offset), category whitelist for incoming news, and duplicate suppression follow upstream `NewsDB`/`NewsPool`. The next publication rotates into the advertised seed `news` DNA attribute once per announcement cycle in the YaCy `b\|` wire form, and attachments arriving on hello callers and greeted peers' seeds are decoded into the incoming queue. Upstream's redundant creation-date reparse check in `PeerActions.processPeerArrival` is not replicated because the Go codec parses the creation date once. |
| `peers/operation/yacyVersionTest` | ported (filter subset) | `yacynode/internal/seedlist/minversion_yacy_fixture_test.go` ports the combined-version vectors against seedlist `minversion` filtering: valid `d.dddSVVV` values order numerically, malformed values never pass. The upstream `combined2prettyVersion` display split has no Go counterpart because this node never renders pretty version strings. |
| `peers/graphics/WebStructureGraphTest` | ported | `yacynode/internal/yagonode/host_link_source_test.go`: the incoming host graph is counted from stored document outlinks per source host and keyed by YaCy host hashes, so several links from one document to the same target host accumulate into one reference count, matching the upstream host-structure facts. Like upstream, only locally parsed documents feed the structure; DHT-received metadata does not. |
| `peers/graphics/ProfilingGraphTest` | not applicable | Server-side UI graphics rendering. |
| `cora/document/id/DigestURLTest` | pre-existing | `yacymodel/url_hash_test.go`: `TestYaCyDigestURLDefaultPortNormalformFixtures` (identPort), `TestYaCyDigestURLHostHashFixtures` (hosthash equality and inequality), `TestYaCyDigestURLFileHashFixtures` (file URL slash variants). |
| `cora/document/id/MultiProtocolURLTest` | pre-existing | `yacymodel/url_hash_test.go`: session-ID removal, backpath, and normalform fixture sets. |
| `cora/document/id/DigestURLHashPerfTest` | not applicable | Performance benchmark, no protocol assertions. |
| `cora/date/GenericFormatterTest` | ported (wire subset) | `yacymodel/seed_time_yacy_fixture_test.go` pins the `yyyyMMddHHmmss` SHORT_SECOND wire form used by seed `BDate`/`LastSeen` with the upstream sample values. The other upstream patterns (SHORT_DAY, SHORT_MINUTE, MILSEC, RFC1123, ANSIC, SIMPLE) do not appear on this node's implemented wire surfaces. |
| `cora/protocol/DomainsTest` | pre-existing (model subset) | Seed addresses arrive as separate `IP`/`Port`/`IP6` DNA attributes handled by `yacymodel/seed_host_test.go`, `seed_port_test.go`, and `seed_address_test.go`; this node has no scheme-stripping helper because it never parses `scheme://host:port/path` peer addresses. |
| `kelondro/data/word/WordReferenceVarsTest` | not applicable | min/max aggregation of Java ranking variables inside `WordReferenceVars`; this node does not reimplement that merge structure. Posting field integrity is covered by the reference-container port below. |
| `kelondro/rwi/ReferenceContainerTest` | ported | `yacynode/internal/rwi/posting_directory_yacy_fixture_test.go`: a posting stored for a word hash is retrievable by URL hash with every ranking property preserved, mirroring the upstream distance-preservation assertion. |
| `search/query/QueryGoalTest` | ported | `yacynode/internal/searchcore/query_yacy_fixture_test.go` passes the upstream include-string vectors verbatim: double- and single-quoted phrases stay whole, plus and minus prefixes are handled, and the upstream position quirks are reproduced. `yacymodel/hash_yacy_fixture_test.go` additionally proves case-insensitive word hashing with apostrophes preserved, so the DHT word-hash set matches upstream. |
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
servlet sources and observed responses.

Functional end-to-end compatibility is proven by the live interop matrix in
`yacynode/test/e2e` against a real `yacy/yacy_search_server` container:

- `TestRealYaCyPromotesNodeToSenior`: mutual hello with the full advertised
  seed DNA; the Java peer back-pings this node and publishes it as a senior.
- `TestRealYaCyTransfersRWIToFleet`: the Java peer crawls a pushed document
  and DHT-transfers RWI postings with URL metadata into this node. Against the
  current upstream `latest` image this scenario does not complete within the
  test window and is under investigation; inbound transfer acceptance itself
  stays covered by the golden transferRWI/transferURL fixtures and endpoint
  tests.
- `TestNodeDistributesRWIToRealYaCy`: this node runs its outbound DHT gates,
  selects the Java peer, and hands off stored RWI and URL metadata through the
  two-phase transferRWI/transferURL exchange until the Java index grows.
- `TestGlobalSearchFindsRealYaCyResults`: a global search on this node fans
  out to the Java peer over `/yacy/search.html` and returns a document that
  only the Java peer has indexed.
