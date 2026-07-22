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
| `peers/NewsPoolTest` | ported (pool level) | `yagonode/internal/peernews/news_pool_test.go` ports the publication fixture: three published records are each offered thirty times through the outgoing queue and then rest in the published queue with the full distribution count; the record wire form, id composition (exact creation second plus originator hash with the `#` offset), category whitelist for incoming news, and duplicate suppression follow upstream `NewsDB`/`NewsPool`. `news_retention_test.go` additionally pins upstream's 1 KiB record ceiling and 24-hour default versus 72-hour profile-update/crawl-start lifetime, with bounded newest-created retention, restart persistence, legacy-marker migration, and invalid/orphan suppression. The next publication rotates into the advertised seed `news` DNA attribute once per announcement cycle in the YaCy `b\|` wire form, and attachments arriving on hello callers and greeted peers' seeds are decoded into the incoming queue. Upstream's redundant creation-date reparse check in `PeerActions.processPeerArrival` is not replicated because the Go codec parses the creation date once. |
| `peers/operation/yacyVersionTest` | ported (filter subset) | `yagonode/internal/seedlist/minversion_yacy_fixture_test.go` ports the combined-version vectors against seedlist `minversion` filtering: valid `d.dddSVVV` values order numerically, while missing, malformed, and numeric-zero peer versions remain eligible as upstream developer-version values. Request floors use Java `Float.parseFloat` syntax and binary32 rounding; stored seed versions use Java `Double.parseDouble` syntax and binary64 rounding. The upstream `combined2prettyVersion` display split has no Go counterpart because this node never renders pretty version strings. |
| `peers/graphics/WebStructureGraphTest` | ported | `yagonode/internal/yagonode/host_link_source_test.go`: the incoming host graph is counted from stored document outlinks per source host and keyed by YaCy host hashes, so several links from one document to the same target host accumulate into one reference count, matching the upstream host-structure facts. Like upstream, only locally parsed documents feed the structure; DHT-received metadata does not. |
| `peers/graphics/ProfilingGraphTest` | not applicable | Server-side UI graphics rendering. |
| `cora/document/id/DigestURLTest` | pre-existing | `yagomodel/url_hash_test.go`: `TestYaCyDigestURLDefaultPortNormalformFixtures` (identPort), `TestYaCyDigestURLHostHashFixtures` (hosthash equality and inequality), `TestYaCyDigestURLFileHashFixtures` (file URL slash variants). |
| `cora/document/id/MultiProtocolURLTest` | pre-existing | `yagomodel/url_hash_test.go`: session-ID removal, backpath, and normalform fixture sets. |
| `cora/document/id/DigestURLHashPerfTest` | not applicable | Performance benchmark, no protocol assertions. |
| `cora/date/GenericFormatterTest` | ported (wire subset) | `yagomodel/seed_time_yacy_fixture_test.go` pins the `yyyyMMddHHmmss` SHORT_SECOND wire form used by seed `BDate`/`LastSeen` with the upstream sample values. The other upstream patterns (SHORT_DAY, SHORT_MINUTE, MILSEC, RFC1123, ANSIC, SIMPLE) do not appear on this node's implemented wire surfaces. |
| `cora/protocol/DomainsTest` | pre-existing (model subset) | Seed addresses arrive as separate `IP`/`Port`/`IP6` DNA attributes handled by `yagomodel/seed_host_test.go`, `seed_port_test.go`, and `seed_address_test.go`; this node has no scheme-stripping helper because it never parses `scheme://host:port/path` peer addresses. |
| `kelondro/data/word/WordReferenceVarsTest` | not applicable | min/max aggregation of Java ranking variables inside `WordReferenceVars`; this node does not reimplement that merge structure. Posting field integrity is covered by the reference-container port below. |
| `kelondro/rwi/ReferenceContainerTest` | ported | `yagonode/internal/rwi/posting_directory_yacy_fixture_test.go`: a posting stored for a word hash is retrievable by URL hash with every ranking property preserved, mirroring the upstream distance-preservation assertion. |
| `search/query/QueryGoalTest` | ported | `yagonode/internal/searchcore/query_yacy_fixture_test.go` passes the upstream include-string vectors verbatim: double- and single-quoted phrases stay whole, plus and minus prefixes are handled, and the upstream position quirks are reproduced. `yagomodel/hash_yacy_fixture_test.go` additionally proves case-insensitive word hashing with apostrophes preserved, so the DHT word-hash set matches upstream. |
| `server/serverObjectsTest` | not applicable | Upstream test prints encodings and asserts nothing. |
| `cora/document/feed/RSSFeedTest` | pre-existing (endpoint level) | `/yacy/urls.xml` feeds are covered by `yagonode/internal/crawlurls/*_test.go` golden shapes. |
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
golden wire fixtures under `yagonode/test/fixtures/yacywire/` and endpoint
tests in the matching `yagonode/internal/*` packages, derived from upstream
servlet sources and observed responses.

Functional end-to-end compatibility is proven by the live interop matrix in
`yagonode/test/e2e` against the pinned stock-Java image
`docker.io/yacy/yacy_search_server@sha256:4225dd07b605347b62ff1fbfa0268217aa79ba2d29bdb0a76d5366d4267398da`.
`YAGO_YACY_IMAGE` can select another explicitly pinned test image.

- `TestRealYaCyPromotesNodeToSenior`: mutual hello with the full advertised
  seed DNA; the Java peer back-pings this node and publishes it as a senior.
- `TestRealYaCyTransfersRWIToFleet`: the Java peer crawls a pushed document
  and DHT-transfers RWI postings with URL metadata into this node. The harness
  raises the Java peer's busy-thread load thresholds because its peer ping and
  DHT jobs skip their cycles entirely on hosts whose load average exceeds the
  upstream defaults, which a many-container test host always does.
- `TestNodeDistributesRWIToRealYaCy`: this node runs its outbound DHT gates,
  selects the Java peer, and hands off stored RWI and URL metadata through the
  two-phase transferRWI/transferURL exchange. The test waits for the unique RWI
  row and then requires `/yacy/urls.xml` to return the exact transferred URL
  GUID, so index growth alone cannot satisfy the proof.
- `TestGlobalSearchFindsRealYaCyResults`: a global search on this node fans
  out to the Java peer over `/yacy/search.html` and returns a document that
  only the Java peer has indexed.
- `TestRealYaCyGlobalSearchFindsYagoRWI`: a Java global search returns a
  Yago-only RWI document from this node. The response carries the transient
  enhanced-base64, fixed-order 20-column `wi` row required by the stock Java
  consumer without modifying stored URL metadata.

Focused hello and roster regressions additionally pin behavior that upstream
implements in `hello.java`, `Protocol.hello`, `PeerActions.connectPeer`, and
`SeedDB.removeMySeed`: a successful callback may report the caller's advertised
hostname in `yourip`; direct `seed0` refreshes peer metadata at the contacted
endpoint; the primary `IP` host and ordered `IP6` entries form a deduplicated
five-host transport bound, IPv6-only seeds remain callable, all attempts share
one operation deadline, and an alternate host becomes primary only after the
responder hash matches; the immutable local hash is rejected and removed from
every peer collection; another peer sharing the same address is not mistaken for self;
authentication failure and self identity return a virgin response without
seeds; a reachable principal remains principal; and the known-peer request is
bounded to the 100 currently reachable rows ordered by local last-seen time and
hash, with Java BMP decimal digits and ASCII signs accepted for `count`, invalid
or overflowing values defaulting to zero, and a nonpositive count returning
none. The raw seed accepts at most 16,000 Java `String.length()` UTF-16 units.
Across every authenticated request adapter, missing network names default to
`freeworld` and explicitly empty names are not defaulted. The hello path selects
from the bounded in-memory reachable set rather than scanning the persistent
roster.
Process-local self classification starts virgin, follows the strongest
current authenticated hello evidence, and retains its last published type when
the current evidence window becomes empty.

The matrix covers the default `freeworld` unit and same-name peers. Controlled
private networks can select Java YaCy's `salted-magic-sim` calculation with one
nonempty shared secret; focused outbound and inbound protocol tests cover that
mode even though the stock-container matrix remains on `freeworld`.

Yago computes URL hashes without DNS. Stock Java YaCy can classify an
unresolvable hostname under an unrecognized top-level domain as local and
produce a different URL hash. The exact-GUID live transfer fixture therefore
uses a recognized public top-level domain. Resolving DNS during hashing would
make identity depend on network state and is not adopted.
