# YaCy-Compatible Go Peer — Technical Specification

## Context

The project implements YaCy-compatible node behavior in Go in small
compatibility-preserving slices. The target is a practical self-hosted
YaCy-compatible search peer that can join the YaCy peer-to-peer network,
exchange RWI and URL metadata, crawl configured sites, serve local and federated
full-text search, expose a Tavily-compatible Search API, and provide
operational and administrative surfaces.

The original lightweight RWI node remains the baseline implementation, not the
final product boundary. Compatibility means preserving YaCy wire protocol
shapes and observable peer behavior where Go code implements the same feature.
Go internals do not need to copy Java source code, Java storage engines, Solr,
Lucene, or Kelondro.

RWI is a compatibility and exchange layer, not the primary local search engine.
The local search target is a document store plus a full-text backend abstraction
with a modern production backend and a pure-Go fallback.

## Target Architecture

```text
yago-node
  - YaCy /yacy/* compatibility
  - peer roster, seedlists, liveness, DHT inbound/outbound
  - RWI vault + URL metadata vault
  - P2P policy, quotas, metrics

yago-searchd
  - local full-text index
  - search backend: embedded Bleve (pure Go), tuned for web search
  - document store
  - snippets, phrase/proximity, filters, facets
  - Tavily-compatible POST /search
  - YaCy-compatible /yacysearch.json and /yacysearch.rss adapter

yago-crawler
  - persistent crawl frontier
  - HTTP fast fetch path
  - optional browser slow fetch path
  - robots.txt, sitemap, canonicalization, politeness
  - content extraction and deduplication
  - emits DocumentIngest + RWI postings + URL metadata

yago-admin-ui
  - React/Next.js or Vite React
  - IBM Carbon UI framework
  - admin functionality comparable to original YaCy categories
```

## Non-Goals

* Copying Java YaCy source code into this repository.
* Requiring Java, Solr, Lucene, Elasticsearch, Kelondro, or Java runtime
  services for core Go peer operation.
* Treating RWI as the only or primary local full-text search index.
* Turning the Tavily-compatible API into a proxy for a paid external search
  service; there is no outbound upstream Tavily integration.
* Copying servlet-style YaCy UI internals into the admin UI.
* Claiming unsupported YaCy endpoints as compatible; incomplete surfaces must be
  explicit in documentation and status output.
* Executing remote crawl work without an explicit safety policy.

## Functional Requirements

* The node SHALL advertise one YaCy Senior peer identity.
* The node SHALL persist an automatically generated YaCy peer hash and name when
  operators do not configure them, and SHALL allow operators to pin either value.
* The node SHALL allow operators to configure the public host and port advertised in its YaCy seed.
* The node SHALL allow operators to configure the public endpoint used for YaCy-compatible reachability self-tests.
* The node SHALL announce itself through configured YaCy seedlists.
* The node SHALL serve YaCy seedlists with upstream-compatible request filters,
  including minimum peer version filtering.
* The node SHALL parse YaCy seed wire forms from configured seedlists without discarding otherwise valid peers over documented or observed `UTC` field variants.
* The node SHALL allow operators to configure a proxy for outbound connections.
* The node SHALL be reachable through one stable public endpoint.
* The node SHALL support peer discovery and peer liveness exchange.
* The node SHALL rotate its next peer news publication into the advertised seed news attribute once per announcement cycle and SHALL accept valid same-network news attachments from arriving peer seeds into its incoming news queue.
* The node SHALL report cumulative counts of words and URLs sent to and received from peers in its advertised seed statistics, and those totals SHALL survive restarts.
* The node SHALL reject peer-liveness callers that present this node's peer hash or advertised endpoint as their own identity.
* The node SHALL announce in peer-liveness responses only its own seed and peers obtained from
  configured seedlists, and SHALL NOT redistribute peers self-reported in inbound requests.
* The node SHALL honor the requested peer count in peer-liveness requests and select the announced
  peers at random.
* The node SHALL receive inbound DHT RWI postings.
* The node SHALL receive URL metadata associated with RWI postings.
* The node SHALL preserve YaCy network-unit authentication behavior for inbound DHT transfer endpoints.
* The node SHALL distribute stored RWI postings and URL metadata to compatible peers when configured.
* The node SHALL verify its DHT reachability through a YaCy-compatible RWI capacity self-test before outbound DHT distribution.
* The node SHALL choose outbound DHT transfer targets using YaCy DHT ring ordering and advertised remote-index capability.
* The node SHALL recover outbound RWI postings selected for DHT handoff after restart when they have not been confirmed as accepted by a compatible peer.
* The node SHALL treat a peer's advertised remote-index capability as authoritative because YaCy transfer rejection values also represent transient load, discovery, and admission states. A protocol rejection SHALL retain peer reachability and use bounded retry readiness instead of rewriting the advertised capability.
* The node SHALL serve remote RWI search requests with receiver-side ceilings of
  10 results and 3,000 milliseconds. When that endpoint-owned deadline expires,
  it SHALL return HTTP 200 with a measured `searchtime`, an empty result set, and
  no partial `indexcount` or `indexabstract`; caller-owned cancellation and
  deadlines SHALL remain errors.
* The node SHALL serve local search requests through YaCy-compatible search surfaces.
* The node SHALL expose YaCy-compatible public search JSON, RSS, HTML, OpenSearch description, and suggestion subsets backed by local full-text search and DHT-selected reachable-peer search where applicable.
* The node SHALL support federated search across local and DHT-selected reachable peer results, using YaCy index abstracts for multi-term remote result conjunctions, filtering remote targets by advertised RWI inventory, and balancing redundant DHT candidates randomly. Global peer-query hashing SHALL preserve every nonblank parsed term because the wire boundary has no reliable document language for language-specific function-word decisions. When swarm morphology is enabled, a multiword query SHALL retain one exact conjunctive primary request. Its bounded abstract recovery MAY address corpus-observed forms and regular forms verified by supported Snowball-rule analyzers, SHALL union forms within each original query requirement, SHALL intersect across every original requirement, and SHALL use the original query for evidence and ranking. It SHALL retain at most 12 forms per requirement, 20 forms across the request, and two peers per form. Candidate generation SHALL NOT claim to identify a single query language. It SHALL retain every applicable rule-backed analyzer identity even when its stem is unchanged or equal to another analyzer's stem, SHALL round-robin proposals across those identities under a 2,048-attempt cap, and SHALL retain a proposal only when its proposing analyzer maps it back to that analyzer's query stem. Duplicate surfaces SHALL collect their distinct verifying analyzer identities. One global order SHALL prefer distinct-analyzer agreement, shorter edit distance and length difference, greater retained prefix and rule support, analyzer priority, and lexical order; the original form SHALL remain first and the result SHALL contain no more than 12 surfaces in total. Rule-based generation SHALL accept only terms from four through 32 Unicode runes and SHALL return only the normalized base form outside that range. After intersection, each peer's metadata requests SHALL use a deterministic greedy cover of terms proven by that peer's own abstract to admit the selected URL set; disjoint term-to-URL sets MAY require more than one request to that peer, and every request SHALL carry only URLs admitted under its exact sent term hash. Primary, abstract, and metadata work SHALL share the existing aggregate remote deadline, actual-attempt ceiling, and response, metadata-row, and abstract-entry budgets. A resource-producing request to a cooperating Yago peer MAY additionally use its negotiated wire requirements, whose hash multiset SHALL match that exact primary wire request, for one strict analyzer-backed candidate search inside the same peer request. A requester MAY map a validated single-word variant ordinal back to its original ranking requirement only through its own one-to-one morphology plan; a peer SHALL NOT supply remapping data. That search SHALL retain at most 32 candidates, SHALL stop after 100 milliseconds, SHALL preserve every wire requirement, and SHALL NOT add a variant request or network round. Stock YaCy peers SHALL retain exact RWI behavior. The rule-derived supplement can address common regular siblings absent from the requester's corpus, but a suppletive or analyzer-unconnected form remains undiscoverable unless it was observed or a cooperating peer supplies analyzer recall.
* Exact local retrieval SHALL build a required-term conjunction for each candidate language analyzer and exempt a word only inside a branch whose analyzer folds it away; every analyzer-position component group of one source term SHALL remain required. Latin-script queries SHALL reach every registered Latin-language analyzer. Ambiguous function words SHALL be verified against the stored document analyzer before admission. Chinese, Japanese, and Korean branches SHALL index mandatory source-offset-preserving character unigrams and overlapping bigrams, so a one-character query and a shorter sequence inside a longer unsegmented run remain searchable. Chinese and Japanese document analyzers MAY add optional dictionary segments as ranking evidence, and Chinese indexing/querying MAY canonicalize only mappings that preserve the original code-point count and byte-span correspondence. Query recall SHALL NOT depend on optional dictionary segments. Pseudo-relevance terms SHALL only reorder exact matches. Fuzzy recovery SHALL run only after a true miss, use the same analyzer-specific conjunction rule, require every retained parsed term through bounded analyzer-consistent edit distance with a shared-prefix floor, disable fuzzy matching for tokens above 64 Unicode runes, and SHALL NOT use document-wide character-gram conjunctions. Distance-two recovery SHALL preserve the first four Unicode runes. Local snippets SHALL use a bounded stored-document evidence scan to center on the matched literal, morphological, or bounded fuzzy surface; a heading-only or trusted-anchor-only match SHALL render evidence from that field. Human search surfaces SHALL mark validated query-match offsets obtained by applying that result's indexed language analyzer to its bounded final snippet. A hydrated local result SHALL retain at most 128 absolute stored-body query spans from the same evidence pass. A local result with body evidence SHALL link the public cached-copy surface to `GET /cached` with exactly one `u`, `analyzer`, `start`, and `end` value and one to 32 repeated `terms` values. The analyzer SHALL contain at most 64 lowercase ASCII letters, digits, underscores, or hyphens; each term SHALL contain at most 256 UTF-8 bytes and all terms at most 4 KiB; `start` and `end` SHALL be decimal byte-offset anchors with `0 <= start < end <= 2^30` and an at-most-8-KiB requested range. The stored anchor SHALL exist and preserve UTF-8 boundaries. The cached passage SHALL expand it by at most 256 source runes on each available side without crossing the 2,048-rune result cap, then return at most 128 analyzer spans relative to the expanded passage. Invalid passage parameters, analyzer identities, or stored ranges SHALL return HTTP 400; an absent passage source or document SHALL return HTTP 404; a backend failure SHALL return HTTP 500. A request carrying only `u` SHALL retain the ordinary full cached-copy behavior. Before lexical ranking, up to the first 500 peer, web, and legacy-RWI candidates without authoritative evidence SHALL analyze only their bounded visible title, snippet, and decoded URL in memory while the request context remains live. A compatible row language hint SHALL select its registered analyzer; a conflicting, absent, or unregistered hint SHALL use script routing, and a script without a registered candidate SHALL use the Unicode-normalizing standard analyzer. Position keys SHALL preserve at most 32 raw query requirements, each field SHALL retain at most 64 positions per requirement, and the snippet SHALL retain at most 128 validated byte spans. Analyzer evidence is authoritative even when it contains no span. Invalid or empty visible text, unavailable analyzer infrastructure, and rows not completed before cancellation or deadline SHALL retain bounded Unicode word-form matching, boundary-aware literal identifiers, and intra-token matching for scripts that do not conventionally delimit every word with whitespace.
* Public search SHALL reject queries above 512 Unicode runes or 32 combined required and excluded parsed terms before retrieval. The interactive search pipeline SHALL enforce a 1.8-second end-to-end processing deadline and preserve completed local results when peer work reaches its deadline. One remote fan-out SHALL resolve one immutable self-seed snapshot for all selected peers. YaCy `resource=local` and admin `scope=local` SHALL never use swarm or external web search. A Tavily-compatible `/search` request SHALL explicitly permit web search after its selected depth. Privacy-permitted `enabled` web search SHALL use an ordered miss cascade: exact/morphological local retrieval with any selected swarm search; one bounded local-exact rescue when that exact stage is incomplete and carries no primary result, or bounded local fuzzy recovery when it completes with an honest miss; then web search if the selected local path also misses. The rescue and fuzzy paths SHALL be mutually exclusive. The `always` mode SHALL start web work alongside local and peer work on every eligible query and SHALL rank-fuse and deduplicate completed primary and web rankings. The complete exact local-plus-swarm and peer-evidence stage SHALL receive at most 600 milliseconds before a sequential fallback or 1,400 milliseconds when web search is already running in parallel; local-exact rescue SHALL receive at most 150 milliseconds except when exact-stage capacity exhaustion selects a capacity-only budget of at most 500 milliseconds; fuzzy recovery SHALL receive at most 150 milliseconds. The complete hedged web stage SHALL receive 900 milliseconds after a miss or 1,500 milliseconds when it starts in parallel. Engine fetch-and-parse work SHALL admit at most eight attempts process-wide. An unavailable engine set SHALL be exposed as a recoverable web partial failure rather than an indistinguishable honest miss. Provider diagnostics SHALL use stable failure categories and SHALL NOT log the submitted query or provider request URL.
* Interactive retrieval SHALL cancel cooperative work before its hard response deadlines so completed partial results can survive. A contended storage view SHALL stop waiting for the global storage gate when its request context ends. Conflicting multi-shard updates SHALL stop new fast-path writer admission and serialize with context-aware writer preference while retaining shared layout access, so ordinary Views continue against committed snapshots; compaction and layout mutation SHALL retain exclusive quiescence. The immutable served-result denylist snapshot SHALL remain available after a completed search stage's context ends. Public search surfaces SHALL admit at most 16 concurrent requests. At most four interactive retrieval pipelines, four retained exact local-plus-peer stages, four local-exact rescue stages, and four retained fuzzy local stages SHALL execute process-wide. An admitted request SHALL wait for an outer pipeline slot only within the existing 1.75-second cooperative context. Exact-stage capacity rescue SHALL wait for its rescue slot only within its 500-millisecond stage context. Other inner admissions SHALL remain nonblocking. A separate nonblocking four-slot admission SHALL bound remote branches and SHALL remain held until remote search itself returns, including after federated retrieval releases an exact-stage slot with a completed local answer. Each outer or inner stage SHALL retain its own admission until its directly wrapped call exits. Only cancellation or a deadline inherited from the caller SHALL return an infrastructure error. Endpoint-owned outer deadline, capacity, and operational failures SHALL preserve any completed response and become classified partial failures; inner exact, local-exact-rescue, fuzzy, remote-stage-admission, and web-provider failures SHALL follow the same recoverable rule so later stages can recover. A page-one or extension refresh classified as incomplete by an outer, exact, local-recovery, remote-stage-admission, or provider infrastructure failure MAY reuse an unexpired nonempty session even when a degraded web-only branch returned rows; a global request MAY also use an equivalent unexpired local session when no exact global session exists. The returned request SHALL retain its requested scope, no synthetic global session SHALL be stored, and reused sessions SHALL carry the current partial failures without replacement, truncation, or TTL extension. A genuine zero-result local answer carrying only ordinary peer failures SHALL remain an honest miss and SHALL replace the session, as SHALL an empty response without failures. Query logging and search metrics SHALL observe the bounded response returned to the caller rather than a late inner completion. When query logging is enabled, incomplete responses SHALL additionally record the total partial-failure count and at most eight ordered unique failure sources; aggregate mode SHALL NOT record query text.
* Completed local, peer, and web rows SHALL be preserved symmetrically when a sibling source fails or loses a cancellation race.
* The served-result denylist SHALL load its immutable snapshot at startup and SHALL publish a changed snapshot after a successful mutation or a durable reconciliation. An add whose durable state cannot be read SHALL fail closed by including the requested entry; an indeterminate remove SHALL retain the prior policy. Request-time filtering SHALL NOT scan persistent storage or iterate every configured domain for each result.
* Optional crawl seeding from web-fallback results SHALL run outside the search response path, admit at most two process-wide background writes, cancel each write after ten seconds, and skip new seed work while admission is saturated.
* Public paging SHALL cache an initial 50-result assembled window plus one materialized lookahead row and extend it on demand in 50-result blocks with the same one-row lookahead up to the 500-result safety horizon. Extensions SHALL preserve the cached prefix order, append only unseen result identities, and keep extending after a short deduplicated window while the backend reports deeper matches. A failure-free response that explicitly closes the backend total or adds no new identity MAY prove exhaustion and reduce the estimate to the materialized prefix. A classified incomplete extension, a response carrying retrieval partial failures, an operational refresh error, or merely reaching the safety horizon SHALL preserve the estimate and remain non-exhaustive. Public portal page links SHALL use only materialized availability, SHALL preserve an explicitly requested non-exhaustive page, and SHALL redirect beyond the last materialized page only after authoritative exhaustion. Each session SHALL publish an immutable visible window so recent-success recovery never waits behind an active deep-page extension. The paging cache SHALL retain at most 128 sessions and 32 MiB in byte-aware LRU order, reconcile extension growth, purge expired entries on access, deeply detach retained payloads, and serve but not retain an entry that exceeds the budget by itself.
* Persistent full-text index search and document hydration MAY run concurrently with ingest, but index, delete, and batch mutations SHALL be serialized to bound concurrent segment memory and preserve mutation completion order.
* Before search listeners open, the persistent Bleve backend SHALL warm the `_analyzer`, title, headings, anchors, body, and URL term dictionaries on every shard by reading field cardinality without a query term, result collection, document hydration, or corpus scan. The warmup SHALL stop opening dictionaries when its fixed 15-second context expires and SHALL aggregate recoverable dictionary open or close failures into one warning.
* Local ranking SHALL build a bounded strict all-term lexical candidate set before document evidence is loaded. A query with at least three distinct terms SHALL additionally build a relaxed branch requiring the ceiling of 60% term coverage; one- and two-term queries SHALL remain conjunctive, and strict matches SHALL rank before relaxed-only matches. Pseudo-relevance expansion SHALL remain bounded and drift-controlled and SHALL NOT reduce either branch's coverage rule. Strict, relaxed, and fuzzy candidate plans SHALL coalesce analyzer scopes only when their actual query-token sequence equals the standard analyzer sequence; an analyzer that transforms or drops a term and every CJK dictionary term SHALL retain a dedicated clause. Serving, explanation, and learned-model training SHALL use the same local retrieval, bounded RM3, and lexical-evidence sequence.
* Candidate retrieval SHALL NOT retain raw document bodies or request Bleve term-vector locations. Disk candidate-only retrieval SHALL use a stored, non-indexed, size-bounded projection for ranking, post-filters, facets, and leading snippets; body-dependent constraints, malformed projections, and compatible legacy indexes MAY fall back to the full stored document. Stored-document evidence SHALL be limited to the leading ten local results per pass, SHALL preserve completed candidates when its deadline expires, and SHALL enrich later visible paging rows without changing their order. Adjacent exact-surface requirements SHALL carry full proximity confidence; analyzer-equivalent requirements SHALL carry lower confidence, preserve original query gaps, and receive no word-form credit during fuzzy recovery. One CJK requirement whose analyzer-position unigram/bigram groups form one coherent contiguous source sequence MAY receive the lower analyzer confidence; alternatives from scattered positions SHALL NOT form evidence, and adjacent exact CJK requirements whose byte offsets touch SHALL remain exact. A quoted phrase SHALL add only a bounded positive preference when its analyzer-normalized terms occupy their original stored position gaps in one field; it SHALL NOT exclude other all-term matches or reorder the unvalidated tail. Explicit position consumers SHALL cap matched locations per field and analyzed component. Final public results SHALL discard ranking-only position maps and learned-only field scores before session caching while retaining the independently capped body query spans; explicit explain requests SHALL retain their field scores and diagnostic trees.
* The live lexical ranking profile SHALL expose exactly 13 operator-safe coefficients from one catalog: title, anchor, heading, URL, and body field boosts; host authority, freshness, content quality, and short-URL priors; ordered and unordered proximity; lexical blend; and original-gap agreement. Saved values SHALL affect the next search, participate in cache identity, and share validation, persistence migration, tuning, and Admin metadata. Local rows SHALL derive term coverage and proximity from analyzer-aligned document positions. Peer, web, and legacy-RWI rows without authoritative positions SHALL derive visible title, snippet, and decoded-URL coverage plus ordered and unordered proximity from the bounded analyzer pass. Only invalid, empty, or un-analyzable visible text, unavailable analyzer infrastructure, and rows whose analyzer pass cannot finish before cancellation or deadline SHALL retain the bounded structural rule. The peer profile scorer SHALL continue to apply its prepared term evidence to title and decoded URL; web URL validation SHALL remain an admission constraint independently of final analyzer evidence. Candidate windows, evidence confidence, relaxed admission, RM3 drift controls, source fusion, diversity, graph damping, safety thresholds, and search deadlines SHALL remain fixed algorithm or safety policy rather than runtime weights. Learned feature weights SHALL change only through held-out model promotion or rollback.
* Learned ranking SHALL run in-process in pure Go on CPU, SHALL NOT require an external API, sidecar, separate trainer, native plugin, or dynamic model runtime, and SHALL preserve a complete lexical path when no learned model is active.
* Learned ranking SHALL use a versioned fixed feature catalog, an explicit evidence-presence mask, bounded model size and candidate windows, persistent active and rollback snapshots, and compare-and-swap activation. Current model formats SHALL exclude missing evidence from robust statistics and linear contributions, build tree thresholds from observed values only, and give a tree zero contribution when its path reaches a missing split. Legacy model readers SHALL preserve their original zero-imputation and tree-routing behavior.
* Learned scoring SHALL reorder at most the configured leading fused candidate window regardless of local, peer, or web provenance. A row without any known ranking evidence SHALL keep its fused slot, and missing evidence SHALL retain the active model format's missing-value semantics. Rows after the candidate window SHALL remain unchanged. Global serving SHALL request the same bounded candidate window as local serving and SHALL NOT add provider calls or over-fetch candidates solely for learned scoring.
* Learned model promotion SHALL compare the proposal with both lexical ranking and the active incumbent on one frozen candidate pool, SHALL require at least 20 independent held-out query clusters and a non-negative 95% cluster-level paired-bootstrap lower bound, SHALL include chronological evidence when timestamps are present, and SHALL reject recall, discounted top-rank safety/spam exposure, named-slice, or rerank wall-latency regressions. Peer-traffic and timeout metrics SHALL be unavailable when not measured and SHALL gate promotion only when both compared arms measured them.
* Click-derived ranking evidence SHALL be tied to a short-lived authenticated impression containing the exposed result identities, positions, and measured propensities; only bounded aggregates SHALL be retained. The response path SHALL wait at most 50 milliseconds for optional impression preparation and persistence and SHALL retain at most four context-insensitive tasks until they return. Capacity, a planning timeout, or a persistence error returned within the budget SHALL preserve the original order without capture metadata; persistence pending at the deadline SHALL continue independently in its retained slot until it returns. A completed task SHALL return its admission slot before publishing its terminal outcome, while shutdown SHALL continue to join the task through outcome delivery or abandonment before storage closes. A click SHALL wait for the matching in-flight persistence outcome before token validation. An emitted token whose persistence fails SHALL remain rejected until expiry in a bounded registry; at registry capacity new impression preparation SHALL fail without issuing a token. A comparable active revision SHALL be team-draft interleaved with the lexical baseline for online comparison; otherwise the node SHALL use adjacent FairPairs. Only statistically confident FairPairs winners SHALL become implicit relevance judgments, and team-draft credit SHALL NOT become qrels.
* Domain authority SHALL use a bounded citation sample and SHALL allow an operator to persist at most 256 canonical trusted domains or IP literals with a TrustRank blend in `[0,1]`. The default trusted set SHALL be empty. Authority, spelling, enabled swarm morphology, and YaCy host-link signals SHALL refresh from one full-corpus pass, and the next periodic pass SHALL wait its complete interval after the previous pass finishes. The pass SHALL briefly fence document admission to capture the last key of both the legacy and admission-ordered document partitions, SHALL read through those boundaries in fixed 16-document raw keyset pages, SHALL release each storage View before document decoding and analysis, SHALL defer later admissions to the next pass, and SHALL NOT make ingest writers yield as though it were an interactive read. Boundary acquisition SHALL honor cancellation. The last successfully completed bounded signal set SHALL replace one atomic vault checkpoint and SHALL load before search listeners open. A checkpoint completed within the refresh interval SHALL delay the first scan only until its original due time; a stale, future-dated, morphology-incomplete, or host-link-incomplete checkpoint SHALL remain usable while an immediate replacement scan runs. Failed or cancelled scans SHALL NOT replace the checkpoint. Policy changes SHALL recompute authority immediately from the retained citation sample without starting another corpus pass or changing the corpus completion time.
* Publication, modification, first-seen, and content-change times SHALL remain
  distinct. An unknown publication time SHALL remain empty through storage,
  result projection, JSON serialization, and human display; fetch or index time
  SHALL NOT be substituted for it, and year 1 SHALL NOT be rendered as a date.
* Duplicate consolidation SHALL use persistent content identities and clusters. Similar unclustered results SHALL NOT be hard-deleted by request-time text fingerprints.
* Document ingest SHALL cluster and index the canonical committed document, including merged lifecycle dates and inbound anchors. Every accepted duplicate URL SHALL remain stored. Admin deletion, quota eviction, redirect purge, and crawl tombstones SHALL remove the document's index, outbound-anchor, and cluster lineage and SHALL refresh any surviving representative.
* Existing URL-keyed document rows SHALL remain readable after upgrade without a bulk migration. New URL admission SHALL append an uncounted composite admission-and-URL row before publishing its URL locator. A missing, malformed, orphaned, or identity-mismatched row SHALL NOT poison later ingest; it SHALL remain invisible and a later ingest SHALL repair it through a new row before replacing the stale locator. Candidate presence checks SHALL validate the expected composite key without decoding the document body. URL and scan-boundary waits SHALL honor cancellation.
* Crawl ingest SHALL order live pages and tombstones by their stable observation identity and time per source URL across separate deliveries. It SHALL persist the newest completed observation after its side effects and before acknowledgement, skip older observations, and acknowledge a committed retry without replaying its effects.
* The node SHALL coalesce at most 16 ready crawl-ingest deliveries for grouped document, index, metadata, posting, stale-sweep, and recrawl work. A partial group SHALL wait no longer than two milliseconds and SHALL stop waiting when its context is cancelled.
* Safety filtering, persistent cluster consolidation, diversity, host crowding, requested date ordering, and paging SHALL run once after learned scoring.
* Peer-supplied results SHALL be capped at the requested row count, and reported remote totals SHALL count deduplicated rows in hand rather than peer-claimed join counts. One federated query SHALL retain at most 8 MiB of peer response data, 1,024 metadata rows, and 8,192 index-abstract hashes across all exact and morphology passes; it SHALL start at most 32 peer HTTP attempts in total. Ordinary peer fetches SHALL additionally admit at most 32 attempts process-wide. Multiword speculative abstract jobs SHALL consume at most 20 of the per-query attempts and additionally share eight process-wide morphology slots inside the ordinary ceiling, so one expanded query cannot occupy every ordinary slot; single-word variant and metadata-cover calls use the total and ordinary process ceilings. Peer response work SHALL reduce through a bounded stream. Local admission or response-budget exhaustion SHALL NOT lower peer reputation. Every emitted peer row SHALL visibly contain exact or single-analyzer morphological evidence for every content term in its title, snippet, or decoded URL. Up to three otherwise-unmatched peer URLs MAY gain visible evidence from bounded cached or fetched page text; matching pages SHALL receive an excerpt containing the complete evidence span. Confirmed mismatches, fetch failures without visible evidence, disabled-fetch rows, and unmatched rows beyond the rescue cap SHALL be removed before zero-result recovery and before web-result fusion, and reported totals SHALL be adjusted; in `always` mode the provider request MAY already be in flight concurrently. Content/script evidence SHALL take precedence over an untrusted peer language label, and all terms SHALL match within one analyzer branch. Fetch concurrency and fetched-text cache memory SHALL be process-wide and bounded. `verify=false` SHALL skip network rescue but SHALL NOT bypass visible-evidence admission; `verify=cacheonly` SHALL never initiate a fetch.
* A resource-producing `/yacy/search.html` request to a Yago peer MAY negotiate namespaced query-match evidence version 1 and SHALL carry the exact normalized wire requirements used to interpret response ordinals. An index-abstract-only request SHALL NOT negotiate evidence. A cooperating peer SHALL derive evidence only from a locally stored document and SHALL key it by the returned resource hash. One request SHALL inspect at most 32 resource candidates, 2 MiB of stored source, 128 KiB of retained base64 wire values, and 100 milliseconds; one document SHALL inspect at most 512 KiB. One resource SHALL retain at most a 2 KiB snippet, 128 snippet ranges, 128 absolute body ranges, five named fields, 32 requirement entries per field, 64 positions per requirement, 256 positions in total, and 16 KiB of JSON before base64 encoding. The receiver SHALL independently validate the registered analyzer, visible-script compatibility, resource identity, exact request ordinals, UTF-8 boundaries, monotonic ranges and positions, allowed fields, and every bound before ranking. A missing, unsupported, malformed, or incompatible extension SHALL leave the existing bounded visible-field analyzer available. For a primary request without a URL allowlist, the serving peer SHALL enable analyzer-backed recall only when the exact query-hash multiset equals the word hashes of those wire requirements; a URL-bounded secondary request SHALL remain confined to its explicit resource allowlist. The serving peer MAY then use the validated wire requirements for its bounded analyzer-backed candidate search, merge analyzer-ranked metadata before deduplicated legacy RWI rows under the ordinary result cap, and then derive evidence independently for the merged rows. Only the requester MAY subsequently apply its local one-to-one single-word ordinal mapping to the original ranking requirement. Unsupported constraints or analyzer-search failure SHALL leave the exact RWI response intact. When duplicate peer rows identify the same URL, rank and reputation contributions SHALL remain fused independently while the retained row upgrades to the strongest authoritative analyzer, snippet, body-range, and field-position payload before final ranking. Stock YaCy requests and responses SHALL remain unchanged.
* Every negotiated query-match evidence item SHALL explicitly list the complete analyzer-relevant ordinal set and the subset absent from all analyzed fields. The position allocator SHALL retain at least one witness for every present relevant ordinal before additional positions. The receiver SHALL reproduce the analyzer-relevant set locally and reject any item whose present and absent ordinals do not form its exact non-overlapping partition.
* The node SHALL expose a Tavily-compatible `POST /search` endpoint backed by exact/morphological local search, YaCy/yago peers where its requested depth includes federation, mutually exclusive bounded local-exact rescue after an empty incomplete exact stage or local fuzzy recovery after an honest miss, and the optional DDGS provider. DDGS SHALL run after the selected local recovery also misses in `enabled` mode or alongside local and peer retrieval in `always` mode. It SHALL be a drop-in Tavily Search API surface: it SHALL return only Tavily-shaped fields, SHALL NOT carry yago-specific provenance markers, and SHALL be search-only, not browsing result pages.
* Tavily-compatible JSON request bodies SHALL be limited to 64 KiB and SHALL contain exactly one JSON value. Raw-content search, extract, crawl, and map SHALL require an authenticated raw scope, admit at most four requests process-wide, run for at most 30 seconds, and cap both retained response data and encoded output at 16 MiB. One live HTML fetch SHALL default to 2 MiB, SHALL have a 4 MiB per-fetch response ceiling, and SHALL reject an over-limit page rather than parsing a truncated document. Crawl and map SHALL attempt at most 200 pages per request; map SHALL retain discovered URLs without page text.
* A default Tavily-compatible search result SHALL contain `title`, `url`, `content`, `raw_content:null`, and a bounded score that decreases with served rank. It SHALL NOT expose internal source provenance. `published_date` SHALL be emitted only for news and normalized to `YYYY-MM-DD`; favicon and image fields SHALL follow their request flags and image entries SHALL use URLs or described objects as requested. A successful response SHALL carry `request_id`. Every error response SHALL contain only `detail.error`, without a request ID or a second Yago error object.
* Tavily-compatible `basic`, `fast`, and `ultra-fast` search SHALL use the local index and `verify=false`; `advanced` SHALL use global local-plus-peer retrieval and `verify=ifexist`. With click-exposure randomization disabled, equivalent root-portal and Tavily `advanced` text requests SHALL preserve the same canonical URL order after cluster deduplication. Equivalence SHALL require the same query, parsed filters, false safe-search policy, result limit, and effective web-fallback consent. Tavily local depths SHALL correspond to the YaCy local surface because the root portal has no local mode. The Tavily score field MAY encode served rank but SHALL NOT reorder the canonical rows.
* Tavily-compatible extract SHALL accept one URL or at most 20 URLs, default to basic Markdown, permit one through five chunks only with a query, and accept a timeout from 1 through 60 seconds with depth-specific defaults of 10 and 30 seconds. Crawl and map SHALL default to depth 1, breadth 20, limit 50, and external links allowed. They SHALL accept depth 1 through 5, breadth 1 through 500, and a positive limit while clipping retained traversal to the node's 200-page cap. Crawl SHALL permit one through five chunks only with instructions. Crawl and map SHALL accept timeouts from 10 through 150 seconds while remaining subject to the node's stricter 30-second hard deadline.
* Tavily-compatible answer generation SHALL remain deterministic and extractive. `auto_parameters` SHALL report normalized topic and depth without intent inference; `country` SHALL be validated without implying a geographic boost; finance SHALL use the general retrieval path. Extract depth SHALL NOT imply a separate extraction engine. Extract queries and crawl instructions MAY select bounded lexical chunks, but instructions SHALL NOT guide traversal and map instructions SHALL NOT alter discovery. The node SHALL NOT claim proprietary semantic reranking, model-guided crawl, upstream Tavily search, image ranking, or real credit accounting.
* API-key lookup and last-used persistence SHALL share a 32-slot process-wide nonblocking admission across admin operations and Tavily-compatible search, extract, crawl, and map. Saturation or authentication storage unavailability SHALL return `503` with `Retry-After`; per-key throttling SHALL return `429` with `Retry-After`. Last-used time SHALL be persisted only after per-key rate-limit admission.
* The node MAY query an external DDGS provider according to one operator mode. `disabled` SHALL not install or call the provider, `explicit` SHALL require request-level consent and run after a miss, `enabled` SHALL automatically run after a miss, and `always` SHALL automatically run alongside local and swarm retrieval for every eligible global query. `always` SHALL NOT weaken local-scope or content-domain gates. The bounded operator-bearing submitted query SHALL be retained separately from the bare local/swarm terms and sent to the provider. Internal Unicode dash punctuation SHALL form word boundaries for local and provider retrieval; a leading ASCII minus SHALL remain exclusion syntax, and structured modifier values SHALL remain intact. Returned rows SHALL be filtered by every structured constraint verifiable from their URL, title, and snippet, including site, TLD, file type, in-URL text, and excluded terms. Internal result provenance SHALL remain `ddgs`; the public portal and Admin SHALL render plain `web`, YaCy HTML SHALL render `[web]`, and Tavily-compatible payloads SHALL carry no provider marker. External web search SHALL be disabled by default and SHALL route outbound queries through the egress guard. Cached provider responses SHALL be normalized to at most 20 rows per query with bounded title, URL, and snippet fields and SHALL share a 4 MiB/256-entry byte-aware cache.
* The node MAY expose, only when an operator explicitly enables it, a public search portal on its public HTTP port, separate from the admin UI, styled after early-2000s Yandex and progressively enhanced so it renders and searches in legacy browsers and on mobile without client JavaScript. It SHALL be disabled by default and SHALL expose only search surfaces, never admin APIs, and SHALL honor the configured query-privacy mode.
* The node SHALL answer YaCy-compatible RWI capacity and status queries, including per-word RWI URL counts and zero-valued wanted-object probes.
* The node SHALL preserve YaCy compact seed compatibility while limiting a decoded seed to 32 KiB, 128 properties, 128 bytes per property key, 8 KiB per generic property or news value, and 256 bytes for its name. Bootstrap import SHALL retain at most 4,096 seeds and 16 MiB. Peer selection SHALL use an owned, mutation-invalidated, context-aware snapshot limited to 4,096 peers and 16 MiB instead of rescanning the complete roster for every search.
* The node SHALL export YaCy-compatible shared blacklist files named in its configured data directory's YaCy settings after peer network-unit authentication.
* The node SHALL export YaCy-compatible peer profile properties from its configured data directory when a profile file exists.
* Shared-blacklist export SHALL admit at most four requests before form parsing and SHALL share a 16 MiB aggregate budget across configuration input, owned list names, list input, and encoded output. Peer-profile export SHALL admit at most four requests, read at most 1 MiB, retain at most 1,024 properties and 1 MiB of owned property data, and encode at most 2 MiB. Either route SHALL return its compatible empty success response instead of partial data after overflow or cancellation.
* The node SHALL export a YaCy-compatible host-link index counted from stored document outlinks per source host, bounded during the shared background corpus pass, retained in its atomic checkpoint, and served from an immutable snapshot without scanning document storage in a peer request.
* The node SHALL run configured crawl jobs and ingest crawler-produced documents, metadata, and postings.
* The crawler SHALL use separate gRPC connections for control traffic and bulk
  ingest. Orders, heartbeats, settlement, and progress SHALL NOT share the
  transport connection used for ingest payloads.
* A crawl order MAY carry explicit automatic-discovery priority. The node SHALL
  persist that priority through lease, requeue, and restart without deriving it
  from a profile name. When discovery priority is enabled, the node SHALL select
  no more than three automatic-discovery orders before a waiting normal order;
  when disabled, it SHALL preserve one global FIFO admission order across both
  priority classes. A current crawler SHALL retain the explicit priority on each
  frontier run and dispatch no more than three due automatic-discovery pages
  before a due normal page. This page selection SHALL remain work-conserving,
  SHALL retain run fairness and value scoring within each class, and SHALL use
  the existing class-neutral scheduler when priority is disabled.
  Every pending payload SHALL remain in the established canonical order bucket;
  secondary priority indexes SHALL contain keys only. An older node SHALL be
  able to drain the complete queue in global FIFO order, and the current node
  SHALL reconcile orders admitted during a downgrade and ignore or remove stale
  index keys for orders already consumed. It SHALL recover the priority of an
  unsettled lease created by the older node from the retained order payload.
* The node and crawler SHALL expose the same automatic-discovery-priority
  bootstrap value. Before opening its order stream, the crawler SHALL make one
  heartbeat attempt bounded to one second. A successful response SHALL apply the persisted
  node value before intake. A failed attempt SHALL leave the crawler bootstrap
  in effect until a periodic heartbeat succeeds and applies the node value live.
* The node SHALL expose a live 1–256 page-fetch-worker setting per connected
  crawler process. It SHALL deliver the latest value over the heartbeat control
  plane, including after a crawler reconnects. The crawler SHALL stop new intake,
  drain the current fetch group, and apply the latest requested size without a
  process restart. This setting SHALL NOT be described or enforced as a limit on
  crawl runs or queued tasks.
* A current crawler heartbeat SHALL carry optional presence for its number of
  occupied page-fetch worker jobs from job start through fetch, parsing, and
  result publication. An explicit zero SHALL remain distinguishable from an
  absent legacy measurement. The node SHALL accept activity only from a worker
  with a registered order stream, count each worker identity once across
  overlapping streams, and remove its activity after the last matching stream
  disconnects. A runtime with no connected crawler SHALL expose known zero
  activity against its configured per-crawler limit. A mixed fleet with any
  connected crawler omitting the measurement SHALL expose activity as unknown.
  Aggregate capacity SHALL equal connected worker identities multiplied by the
  live per-crawler setting. Older nodes SHALL ignore the additive heartbeat
  field, and current nodes SHALL treat an omitted measurement as unknown.
* Crawl progress submission SHALL be nonblocking and ordered by run phase.
  Periodic phases SHALL be distributed across the reporting interval, at most
  one progress RPC SHALL run per worker, adjacent running snapshots MAY
  coalesce, ready terminal heads SHALL take priority, and terminal snapshots
  admitted to the queue SHALL retry with bounded jittered backoff. A NAK
  redelivery MAY reopen the same run identity, and admitted phases SHALL retain
  their `terminal → running → terminal` order through graceful-shutdown drain
  attempts. At the hard queue capacity, a terminal phase SHALL evict only an
  expendable singleton running phase; if every slot belongs to a protected phase
  chain, the new phase SHALL be logged and dropped without collapsing that chain.
* After exact lease, worker, session, and run authorization, the node SHALL reuse
  that authorized run target for control reconciliation and progress recording.
  It SHALL NOT scan the complete lease bucket a second time for the same running
  report. Human-facing run identities derived from byte provenance, including
  crawler progress warnings, SHALL use lowercase hexadecimal text rather than raw
  bytes.
* Fetched and failed crawl progress SHALL be mutually exclusive terminal page
  outcomes. The Admin failure rate SHALL divide failed outcomes by fetched plus
  failed outcomes and SHALL remain bounded from zero through 100 percent.
* Five consecutive typed host-availability failures within one crawl run SHALL
  retire only that host's remaining queued pages. A served response SHALL reset
  the host evidence. URL-specific gone, unsupported-media, ordinary client,
  robots, cancellation, and permanent egress-policy outcomes SHALL NOT retire a
  healthy host. A single-host run SHALL then finish normally, while a multi-host
  run SHALL continue its remaining hosts.
* Recording a crawl state transition SHALL NOT wait for durable event-log I/O.
  Event persistence SHALL use a bounded asynchronous queue and a bounded
  node-shutdown grace. Storage close SHALL wait behind the writer's quiescence
  barrier without extending the service shutdown deadline.
* Node HTTP shutdown SHALL reserve part of its fixed total deadline for forced
  connection close and bounded handler drain. An elapsed graceful phase SHALL
  count as a clean stop only when forced close and handler drain both complete;
  unexpected shutdown, close, and drain failures SHALL remain errors.
* The node SHALL let local crawl dispatch jobs mark start seeds as normal URLs,
  explicit sitemaps, or explicit sitelists.
* Before frontier admission, the crawler SHALL reject a discovered link whose URL path unambiguously names a disabled parser family or a known unsupported container format. Explicit seeds SHALL remain eligible for one fetch, and extensionless routes, unknown suffixes, and suffix-like query values SHALL remain eligible so response media type can decide routing.
* The node SHALL reject remote crawl work unless a configured policy explicitly allows it.
* The node SHALL return YaCy-compatible empty remote-crawl responses while remote crawl work is disabled.
* The node SHALL return YaCy-compatible crawl receipt retry delays while remote crawl work is disabled.
* The node SHALL store accepted RWI postings and the URL metadata those postings reference.
* The node SHALL store canonical URL, normalized URL, title, page description metadata, headings, extracted text, language, content type, fetch status, fetch timestamps, content hash, outlinks, available inlink or anchor metadata, and bounded image URL/alt metadata for locally indexed documents.
* When PDF page structure is available, text extraction SHALL select referenced
  Page `/Contents` streams and only Form XObjects reachable from page resources
  instead of scanning every decoded stream. A document whose Page objects cannot
  be resolved MAY use a bounded fallback after excluding known non-page and binary
  stream classes. Image data, embedded font programs, metadata, object containers,
  cross-reference streams, embedded files, and CMaps SHALL NOT become indexed page
  text.
* For each simple PDF font code, an embedded `/ToUnicode` mapping SHALL take
  precedence. When that code has no usable mapping, the crawler MAY use a bounded
  single-byte decoder that resolves `/Encoding` as a predefined name or an inline
  or indirect dictionary, initializes a private table from a supported
  `/BaseEncoding`, applies `/Differences` within the 256-code space, and derives
  Unicode text from standard glyph-name semantics.
* The simple-font decoder SHALL share the PDF object's reference, cycle, decoded
  byte, and output-text limits. Malformed arrays, invalid scalar names, unknown
  glyph names, unresolved references, and exhausted budgets SHALL leave affected
  codes unmapped. A selected font without a trusted mapping SHALL NOT fall back to
  raw character-code, Latin-1, or heuristic ligature text.
* CMap, font-encoding, and page/Form stream decoding SHALL share a 32 MiB
  per-document work budget, and extracted UTF-8 text SHALL stop at 1 MiB. Existing
  documents SHALL refresh through the normal recrawl lifecycle; repository
  regressions SHALL use bounded synthetic fixtures, external PDFs MAY be used for
  verification only, and the crawler SHALL NOT perform OCR implicitly.
* The node SHALL support a full-text search backend abstraction with indexing, deletion, search, and stats operations.
* The node SHALL use an embedded pure-Go Bleve full-text backend, tuned for web search, behind that abstraction.
* The embedded pure-Go full-text fallback, when selected, SHALL persist its own
  index under the configured node data directory.
* The embedded pure-Go full-text fallback SHALL rebuild from the document store
  only when its own persistent index is missing or unusable. A destructive
  rebuild SHALL persist an in-progress marker before retiring index data, clear
  it only after a complete document-store scan, and restart the full rebuild
  before serving if a prior attempt was interrupted. Rebuild writes SHALL use
  bounded 16-document shard batches.
* The node SHALL generate snippets from the document store where document text is available.
* The node SHALL support bounded quoted-phrase preference plus ordered and unordered proximity evidence through the local stored-position path.
* The node SHALL expose machine-readable compatibility status for implemented and missing YaCy surfaces.
* The node SHALL allow operators to configure its storage quota.
* Concurrent storage-capacity preflights SHALL share one successful live-byte observation for at most one second and SHALL compare it with the current quota on every call. Exact usage reads SHALL remain exact and SHALL refresh the shared observation. A newer exact observation SHALL supersede an older in-flight preflight, failures SHALL NOT be cached, and a cancelled waiter SHALL return promptly. The preflight is advisory; commit-time operating-system capacity failures SHALL remain final backpressure.
* The main-vault quota SHALL remain a soft admission and eviction target for
  logical live rows. It SHALL NOT be described as a filesystem or aggregate data
  limit and SHALL exclude Bleve, node and crawler checkpoint databases, allocated
  free pages, open-but-deleted blocks, and temporary storage-engine copies.
* The node and crawler SHALL expose independent reserved-free and recovery-
  hysteresis bootstrap values with matching live Admin settings. Each process
  SHALL measure the filesystem containing its own data directory, SHALL fail
  gate-managed growth admission closed when measurement is unavailable, and
  SHALL resume only after available space reaches the reserve plus hysteresis.
  Removal, eviction, settlement, and recovery paths SHALL remain available where
  startup has otherwise completed.
* Node compaction and shard splitting SHALL serialize actual-source headroom
  measurement, a forced fresh filesystem observation, admission, and the
  complete copy. Retained legacy-state migration SHALL admit each bounded page
  with payload and allocation headroom. The policy is advisory; a hard aggregate
  maximum requires an operator-provisioned filesystem/project quota or a quota-
  capable volume for each data placement.
* The node SHALL expose operator controls for network, crawl, index, search, and security settings.
* The admin Network page SHALL obtain the complete known peer roster and render
  exactly 20 peers per page while preserving server-side sorting and no-JavaScript
  navigation. A roster of 270 peers SHALL therefore expose 14 pages.
* The admin Crawler monitor SHALL render the unified all-profile run snapshot in
  pages of exactly 20 rows, while totals and health remain based on the complete
  snapshot. Its selected page SHALL survive periodic refreshes and run controls.
* The admin Activity page SHALL render retained searches newest-first in pages of
  exactly 20 rows while lifetime totals and top-word summaries remain based on
  the complete retained activity snapshot.
* Every stylesheet and script referenced by an Admin template SHALL carry a
  revision derived from its embedded content so an upgraded binary cannot reuse
  a fresh cached representation from an earlier release.
* The node SHALL expose stable typed admin APIs for the administration UI.
* The node SHALL expose readiness as a machine-readable operations endpoint
  separate from lightweight health/liveness.
* The node SHALL expose local search index availability, backend, document count,
  and update time through a stable typed admin API.
* Overview and Index SHALL use the local full-text backend's document count as the authoritative local index population. Overview SHALL report YaCy URL metadata records as a separately labelled population.
* The admin UI SHALL use IBM Carbon and SHALL be comparable by category to original YaCy administration without copying the legacy servlet UI.
* Native `yago-v2` P2P, if added, SHALL be optional and SHALL NOT change legacy `/yacy/*` compatibility behavior.

## Non-Functional Requirements

* The node SHALL durably retain accepted records before acknowledging them.
* The node SHALL apply backpressure when it cannot durably retain further accepted records.
* The node SHALL keep memory usage bounded independently of total stored RWI size.
* The node SHALL keep memory usage bounded independently of total document store
  and full-text index size.
* The node SHALL set explicit limits on all caches, queues, buffers, batches, and request bodies.
* Unauthenticated administrator login and setup bodies SHALL be limited to 16 KiB and admitted through one 32-slot process-wide predecode gate shared by JSON and HTML forms. JSON login and setup SHALL require the `application/json` media type. The first-run HTML setup GET SHALL issue a short-lived unpredictable signed token in a host-only `HttpOnly`, `SameSite=Strict` cookie and the rendered hidden field; its POST SHALL validate both copies and the signature and expiry before creating credentials, then clear the cookie. Usernames SHALL be limited to 256 bytes, passwords to 1 KiB, tracked login identities and failures SHALL be bounded, and at most two Argon2 operations SHALL run process-wide.
* Dynamic authentication and administrator HTML SHALL be `private, no-store`. Authentication pages SHALL permit only same-origin static styles and images, SHALL reject framing, and SHALL disable all other content sources. The login page SHALL leave the account name empty and SHALL expose only bounded node name, advertised swarm endpoint, processor model and logical processor count with architecture fallback, total/free memory, configured data-filesystem free space, version, and uptime values; an unavailable individual value SHALL NOT prevent the remaining values from rendering. The authentication stylesheet's exact current content revision MAY be cached as immutable, its canonical unversioned path SHALL revalidate, and every wrong, duplicate, extra, encoded, malformed, or noncanonical revision request SHALL return a no-store `404`.
* The local index-result cache SHALL retain at most 256 entries and 16 MiB in byte-aware LRU order, deep-clone retained payloads, release stale generations immediately after successful index mutations, and serve but not retain an entry that exceeds the budget by itself.
* The node SHALL complete requests within bounded deadlines.
* The node SHALL prefer availability and data integrity over ingestion throughput.
* The node SHALL support low-resource Linux-class devices, including Raspberry-Pi-class hardware.
* The node SHALL preserve compatibility with standard YaCy peer-to-peer contracts.
* The node SHALL NOT require rebuilding the complete index in memory after restart.
* The node SHALL NOT corrupt persistent state when disk is exhausted.
* Storage engines SHALL be replaceable behind a narrow interface.
* Search backends SHALL be replaceable behind a narrow interface.
* Document storage SHALL enforce size limits, retention policy, and security policy before raw content or raw-content references are stored.
* Operational behavior SHALL be observable through machine-readable metrics.
* Security-sensitive behavior SHALL default closed until configured by an operator.
* The crawler SHALL deny private, loopback, link-local, multicast, unspecified, and metadata destinations by default.
* The crawler SHALL protect against DNS rebinding by validating destinations at admission and fetch time.
* The crawler SHALL expand explicit XML sitemap URL sets, sitemap indexes, and
  plain text sitelists into bounded normal URL crawl requests before frontier
  admission.
* The crawler SHALL terminate invalid crawl orders, deterministic malformed seed
  content, permanent admission failures, and explicit operator cancellation. It
  SHALL NAK retryable network, server, throttle, timeout, expansion, and ingest
  delivery failures. Graceful process shutdown SHALL retain unfinished checkpoint
  state and leave its session-aware lease unsettled for same-worker adoption even
  after the process-session deadline;
  it SHALL NOT turn an operator cancellation into redelivery. Transient order
  settlement failures SHALL retry idempotently while heartbeats remain live and
  stop within a bounded shutdown window.
* A NAK SHALL durably clear the lease owner and defer global availability for five
  seconds. The lease sweeper SHALL service that deadline without immediate
  redelivery or sub-second polling. Node restart SHALL preserve an unexpired retry
  deadline. A legacy terminal settlement SHALL remain idempotently replayable for
  24 hours after settlement and SHALL fail at that horizon even when asynchronous
  cleanup has not yet removed its row. A tokenized rich terminal snapshot SHALL
  enter a fixed 24-hour confirmation window only after terminal progress delivery
  is confirmed; an ACK SHALL additionally require durable run-control completion.
  An unfinalized snapshot SHALL NOT enter expiry. Expiry SHALL atomically perform
  any still-pending requeue and remove the finalized settlement in batches of at
  most 256, while a later valid token confirmation SHALL remain idempotently
  successful. An acknowledgement for a lease already requeued by legacy NAK or
  expiry SHALL be rejected.
* The node SHALL preserve every session-aware crawl-order lease, its stable-worker
  ownership, and its lease identity across deadline expiry and node restart.
  Startup SHALL requeue only expired deferred or legacy sessionless leases, and a
  reconnecting process with the same stable worker identity SHALL atomically adopt,
  renew, and receive its leases before new pending work. Each crawler data
  directory SHALL persist one stable worker identity, while each process SHALL
  create a new session identity. The node SHALL reject a concurrent live-session
  takeover before cancellation, adoption, or registry mutation and SHALL fence
  heartbeat, ingest, progress, and settlement mutations by the current worker,
  process session, and lease. Run-targeted controls SHALL survive an offline worker
  and be delivered after reconnect; worker-wide controls MAY remain live-only.
  Every crawler heartbeat RPC SHALL have a one-second client deadline. If an
  active lease is omitted, expires, or otherwise loses its local grant, the
  crawler SHALL cancel and reconnect its order stream so the same worker can
  adopt the parked lease without waiting for another transport failure. An
  ordinary delivery SHALL consume one session-scoped delivery credit after its
  durable claim. The node SHALL NOT claim or send the next order for that session
  until a successful heartbeat renews the current lease or a successful
  session-authorized disposition proves receipt of and completes that exact
  lease. The current crawler SHALL confirm an ordinary lease before decoding its
  payload and SHALL reconnect if settlement of an undecodable payload fails. The
  node SHALL hold neither a database transaction nor a worker-session registry
  lock while awaiting confirmation.
* One worker session SHALL retain at most 1,024 active leases. The node SHALL
  partition adopted leases into ordered recovery batches of at most 16. The first
  message of each batch SHALL carry that batch's complete lease-ID header, and the
  node SHALL send no remainder of the batch until a successful heartbeat renews
  every ID in the header. Before exposing the first recovered order, the crawler
  SHALL validate the header and confirm only that batch. It SHALL then validate
  each streamed lease against the header, require an exact final marker, and
  retain no complete recovery set of order payloads. Periodic heartbeats SHALL
  continue to carry the complete active lease set. A current crawler SHALL accept
  the older single-batch shape up to the 1,024-lease contract ceiling; unmarked
  replay from an older node SHALL retain per-order confirmation.
* When its crawl runtime is enabled, the node SHALL keep crawl orders, priority
  indexes, idempotency, leases, settlement history, controls, and terminal-run
  delivery state in one atomic bbolt database at
  `${YAGO_DATA_DIR}/crawlbroker.db`. The file SHALL be separate from the main
  sharded vault and SHALL NOT count toward `YAGO_STORAGE_QUOTA` or participate
  in main-vault eviction or compaction. Until a separate crawler-state limit is
  implemented, the file has no application byte cap.
* Crawl-broker startup SHALL rebuild an in-memory active-lease catalog from the
  durable lease bucket. Capacity checks SHALL use an O(1) worker/session lookup,
  and committed claim, adoption, settlement, defer, and requeue transitions SHALL
  update the catalog while the durable bucket remains authoritative across
  restart.
* The dedicated bbolt engine SHALL serialize write admission. An RPC waiting for
  writer admission SHALL stop when its context ends, and an admitted write SHALL
  check cancellation before its transaction callback and again before commit so
  cancelled work rolls back. Provisioning that has no request context MAY wait
  unboundedly. Read transactions retain their existing behavior.
* The first dedicated-state startup SHALL retain-migrate one frozen version-1
  bucket set from the legacy node vault before listeners open. It SHALL copy at
  most 256 ordered rows per target transaction, commit each migration cursor
  atomically with its page, verify source and target fingerprints, and commit a
  marker bound to the migration version and bucket set. An interrupted migration
  SHALL resume; a conflicting row, fingerprint mismatch, or marker mismatch
  SHALL fail startup. An absent source bucket SHALL be treated as empty without
  provisioning or otherwise mutating the source, and an engine that cannot
  report bucket presence SHALL fail closed. The source SHALL remain unchanged
  after cutover. The migration SHALL copy, not claim to repair, any historically
  inconsistent legacy state.
* A completed dedicated migration SHALL make `crawlbroker.db` authoritative for
  later current starts. Operators SHALL NOT delete only that file or downgrade
  in place: either action can read the retained stale cutover rows and resurrect
  settled work. Rollback requires a coordinated stopped backup of the complete
  node and crawler data with matching older binaries. Opening the dedicated file
  SHALL wait no more than five seconds for its exclusive lock. A graceful node
  stop SHALL close the broker before the file; after a crash, restart SHALL expose
  only committed bbolt transactions. Pending and deferred work, controls,
  settlement history, and session-aware leases SHALL retain their existing
  restart rules. Creating a missing or zero-length dedicated file SHALL reserve
  bounded initialization headroom through a fresh serialized pressure check; a
  valid existing file SHALL remain openable under pressure for recovery.
* The node SHALL expose dedicated crawler-state live-use and allocated-file
  gauges as `crawl_broker_state_used_bytes` and
  `crawl_broker_state_file_bytes`. The allocated size MAY remain above live use
  after deletions.
* Before exposing work for dispatch, the crawler SHALL persist the complete ordered
  seed manifest and admission cursor, exact normalized visited set, ordered
  outstanding pages, per-host admission and pace state, controls, run completion,
  redirect ownership, per-page ingest observation identity, and terminal
  settlement outbox. A process restart SHALL resume that state without fetching a
  committed page again or reparsing a completely published seed manifest.
* A newly admitted run SHALL publish one running-state report with the immutable
  seeded queue depth before terminal settlement can publish a finished or
  cancelled state. Periodic running reports SHALL use the live pending depth and
  SHALL NOT follow a terminal report.
* Production checkpoint recovery SHALL materialize at most 256 persisted pages per
  active run, refill only the unused portion of that live window after it drops
  below 128 pages, and query exact persisted visited, host-retirement, host-total,
  and run-total state in candidate batches instead of loading those sets. Every
  committed seed or discovered-page admission SHALL extend the durable recovery
  boundary by its exact accepted count and enter the same cursor. Only scalar
  in-memory totals SHALL advance before the admission commit; accepted pages SHALL
  NOT enter resident visited, redirect, host, ready, or pending maps directly. An
  unfinished seed manifest SHALL use the same bounded producer. Completion,
  cancellation, and host retirement SHALL evict cold resident state. The
  completion count SHALL include unloaded pages, and checkpoint reads SHALL occur
  outside the global frontier mutex.
* A discovered child SHALL become durable before its parent page is completed.
  Graceful shutdown SHALL retain unfinished frontier state until the replayed run
  reaches a successful terminal acknowledgement or explicit cancellation.
* The crawler SHALL reject seed and redirect identity URLs longer than 2,048
  bytes before artifact construction and SHALL drop overlong referenced URL
  elements rather than truncate them into different identities.
* The crawler SHALL carry sitemap `lastmod` values as recrawl scheduling hints.
