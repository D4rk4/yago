# Compatibility Status

This project is a Go YaCy-compatible peer in progress. It does not claim full
Java YaCy Search Server compatibility. Compatibility is implemented and verified
surface by surface.

As of 2026-07 the surface-by-surface review is complete: every mounted surface is
either `implemented` (served with tests) or carries a recorded `partial` /
`unsupported` decision — none is unfinished work. The surfaces that remain
`partial` are narrower by deliberate scoping, not pending: remote crawl is a
default-deny opt-in available only on a salted authenticated controlled network
with exact peer and destination allowlists, live fetch-on-extract is
operator-controlled and disabled by default, Tavily answers are deterministic
extraction rather than model generation, and admin crawl dispatch depends on
crawler integration being configured.

The ops listener exposes the same status as JSON:

```sh
curl -fsS http://127.0.0.1:9090/api/admin/v1/compatibility
```

Status values:

- `implemented`: the current node serves the surface with tests for the stated
  behavior.
- `partial`: the wire shape exists, but behavior is intentionally narrower than
  Java YaCy or depends on configuration.
- `planned`: the endpoint or behavior is a project target but is not mounted.
- `unsupported`: the endpoint or behavior is not a project target.

Every authenticated YaCy form endpoint preserves whether `network.unit.name`
was absent or explicitly empty. Absence defaults to `freeworld`; explicit
emptiness remains empty and fails authentication against `freeworld`; an empty
local configuration still denotes `freeworld`. Hello additionally rejects a raw
seed above 16,000 Java `String.length()` UTF-16 units before generic decoding.

## YaCy Peer Protocol

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Peer liveness handshake | `/yacy/hello.html` | GET, POST | implemented | `iam` is an opaque wire value and salted authentication uses its exact value. `count` follows Java signed-decimal int32 parsing, including BMP decimal digits and ASCII signs; missing, malformed, supplementary-digit, or overflow input falls back to zero. A missing network name defaults to `freeworld`, while a present empty name remains empty and is rejected against `freeworld`; an empty local configuration still denotes `freeworld`. Authentication failure and self identity return `yourtype=virgin` with no seeds. An authenticated non-self response returns caller IP, the locally observed caller type, own seed, and at most 100 currently reachable seeds in descending local last-seen order with a hash tie-break; a nonpositive requested count returns no known seeds. This bounded selection reads in-memory reachable membership and does not scan the persistent roster per hello. A successful callback classifies a principal caller as principal and another caller as senior, while failure classifies it as junior. A caller with a usable advertised endpoint is retained under that result: successful callers enter bounded reachable membership, while failed callers remain persisted `junior` potential peers visible to Admin but excluded from reachable membership, hello and seed-list export, search candidates, and DHT targets. Callback work uses one upstream aggregate 6.5-second HTTP or 13-second HTTPS-first boundary across at most five unique advertised hosts, requires a nonnegative RWI-count response, replaces unspecified advertised literals with a usable trusted transport address, and persists the result under a separate bounded context. Transport orders the primary `IP` host before `IP6` entries, supports IPv6-only seeds, brackets IPv6 URI authorities, and promotes the host that answered before persistence. Outbound hello applies the same bounded address order and accepts a usable IP or the local seed's matching advertised DNS name in `yourip`; only a matching responder hash may refresh metadata at the contacted host. DNS aliases never merge peer identity. The persistent roster excludes the immutable local hash across admission, selection, lookup, counts, and Admin projection. External hello evidence publishes the self seed as virgin at process start, junior after failure-only current evidence, or senior after any current success; an empty current window retains the last published type. Self seed, seed-list output, Admin Overview, and DHT maturity use that one classification. |
| RWI and URL count query | `/yacy/query.html` | GET, POST | implemented | Treats `iam` and `youare` as opaque wire values, authenticates with the exact supplied `iam`, and accepts only the exact local hash as the target. It answers `rwicount`, per-word `rwiurlcount`, the stock constant `lurlcount=1`, and zero-valued `wanted*` probes. An authenticated unknown object or failed per-word RWI read returns stock's literal unresolved response marker with the node-start `magic` and current fourteen-digit `mytime`; an unauthenticated request returns `-1`, the same `magic`, and the template's unresolved `mytime`. The response parser accepts the exact unresolved response marker while rejecting any other malformed count. The route retains the stock HTML extension's `text/html` content type without changing other key-value protocol endpoints. |
| Inbound RWI transfer | `/yacy/transferRWI.html` | POST | implemented | Checks the YaCy network unit and required transfer fields before intake, requires the authenticated sender hash to exist in the persistent roster, refuses with `not_granted` when the sender is unknown or the operator turned the accept-remote-index capability off, and accepts at most 1,000 declared and actual RWI rows. Known inactive and junior senders remain eligible. Admission-gate saturation returns YaCy's parseable HTTP 200 `too high load` answer. An oversized transfer or storage-capacity, cancellation, or pre-commit deadline pressure returns HTTP 200 `busy` with a millisecond `pause`, so a sender can respect the receiver's requested backpressure interval. Accepted rows are durable before acknowledgement, and the response reports missing URL metadata. |
| Inbound URL metadata transfer | `/yacy/transferURL.html` | POST | implemented | Checks the YaCy network unit before target handling, requires the authenticated sender hash to exist in the persistent roster, and refuses with `error_not_granted` when the sender is unknown or the operator turned the accept-remote-index capability off. Known inactive and junior senders remain eligible. It accepts at most 1,000 declared URL rows and rejects a larger count before allocating per-row storage. After successful parsing, admission, storage-capacity, cancellation, or pre-commit deadline pressure returns the endpoint's parseable HTTP 200 not-granted answer. Accepted rows are durable before acknowledgement. RWI-to-URL reconciliation metrics use a process-local 65,536-hash FIFO observation set: a newly stored identity increments once, an existing identity only releases pending state, and a rejected identity remains pending. Eviction or restart can omit a metric match but cannot change stored data. |
| Remote RWI search | `/yacy/search.html` | GET, POST | implemented | Serves key-value YaCy remote search responses from local RWI storage (never Solr — see the swarm interop note below), clamps requested count to 10 and time to 3,000 milliseconds like YaCy, retains at most 32 required hashes, 32 excluded hashes, 32 requested abstract hashes, and 128 URL hashes, and sheds concurrent floods with empty-but-valid responses. Author metadata containment, full-match URL filtering through Go's linear-time RE2 implementation, file extension, and URL scheme are applied to the bounded joined set before top-k selection. Its own search deadline produces a parseable HTTP 200 empty answer with measured `searchtime` and no partial counts or abstracts; caller-owned cancellation remains an error. Resource rows carry an enhanced-base64 `wi` containing the complete fixed-order 20-column YaCy `WordReferenceRow` property form. The field is attached only to response copies and never persisted into URL metadata. Outbound searches carry the node's current compact seed in `myseed`, its matching hash in `iam`, and every directly representable search operator. One query starts at most 32 physical peer HTTP attempts; alternate addresses split the remaining deadline, and a response-body I/O failure may fall through while size, parse, abstract, and WordReference failures do not. Resource, index-abstract, and secondary responses share one lifecycle reduction in which success wins over transport failure and invalid protocol data changes reputation without changing reachability. A valid authenticated `myseed` may add one transport-address-bound virgin roster candidate; it does not claim reachability or overwrite a known peer, and ordinary hello later verifies it. Wire-conformance and live stock-Java tests prove that compatible peers can parse and find these responses. |
| Seed list | `/yacy/seedlist.html` | GET, POST | implemented | Serves upstream selector precedence: `my` key presence returns self; otherwise `id` key presence precedes `name`, and targeted lookups ignore regular filters. Malformed or empty `id` selects no peer. Remote name lookup follows YaCy lowercase and angle-bracket normalization; the final self-name fallback is raw and case-insensitive. The `.yacy` suffix and `localpeer` alias are recognized without trimming other whitespace. Regular selection optionally places self first, then confirmed active peers, then recent passive peers. `maxcount` uses the shared Java signed-decimal int32 parser, accepts BMP decimal digits and ASCII signs, rejects supplementary digits, falls back after malformed or overflow input, and is capped at 1,000; `node`, `me`, and `address` recognize only true/on/1 as true. The `minversion` request uses Java `Float.parseFloat` syntax and binary32 rounding; stored seed versions use Java `Double.parseDouble` syntax. The filter applies only to active peers and preserves missing, malformed, or numeric-zero peer versions as upstream developer versions. Plain output uses the shorter stock `b|` or `z|` compact form, preserves unique primary-before-alternative addresses, retains the HTML extension's `text/html` content type, and ignores the XML/JSON-only `peername` filter. Configured bootstrap import accepts seed `UTC` offset and timestamp wire values. Inbound seeds are limited to 32 KiB, 128 properties, 128-byte keys, 8 KiB generic/news values, and a 256-byte name. One bootstrap refresh uses at most eight concurrent fetches and one ten-second deadline, preserves sources completed before that deadline, rejects future or older-than-24-hour observations, deduplicates by freshest hash, and retains at most 4,096 seeds/16 MiB across all sources. Search and seed-list directory selection reuse an owned active-first 4,096-peer/16 MiB mutation-invalidated roster snapshot; name lookup never scans the unbounded persistent roster. |
| Seed list JSON | `/yacy/seedlist.json` | GET, POST | implemented | Serves the same optional self, confirmed active, then recent passive selection as the plain seed list, with the same YaCy request filters. A present `callback` wraps the body as JSONP, including an empty callback value, while retaining stock's extension-derived `application/json` content type. |
| Seed list XML | `/yacy/seedlist.xml` | GET, POST | implemented | Serves the same optional self, confirmed active, then recent passive selection as the plain seed list, with the same YaCy request filters. |
| Bootstrap seeds | `/p2p/seeds` | GET, POST | implemented | Serves the plain CRLF seed-string list at upstream's unauthenticated bootstrap path (the same list principal peers upload to a bootstrap position), with stock's HTML-extension content type and the shared seedlist filters (`maxcount` capped at 1000, `minversion`, `node`, `me`, `address`, `my`, `id`, `name`, `peername`). |
| Bootstrap seeds JSON | `/p2p/seeds.json` | GET, POST | implemented | Serves the peers-array JSON bootstrap shape (hash-first seed maps plus public `Address` entries, JSONP `callback` supported) from the same backend as `/yacy/seedlist.json`. |
| Host-link index | `/yacy/idx.json` | GET, POST | implemented | Serves the `object=host` shape with an incoming host-link index counted from stored document outlinks per source host, advertising the exact `String h-6, Cardinal m-4 {b256}, Cardinal c-4 {b256}` rowdef and emitting each reference in YaCy's `toPropertyForm(':')` shape. The completion-relative background corpus pass stops at 4,096 target hosts, 64 source hosts per target, and 32,768 total references before further graph entries are allocated, persists the graph with the other corpus signals, and publishes an immutable snapshot. Endpoint intake admits four requests, and each request only reads that snapshot; peer traffic never starts or waits for a document-store scan. `object=host` is upstream `idx.java`'s only implemented object (verified against `source/net/yacy/htroot/yacy/idx.java` + `WebStructureGraph.java`, 2026-07). |
| Shared blacklist export | `/yacy/list.html` | GET, POST | implemented | Checks the YaCy network unit and serves `col=black` from files named in `YAGO_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`, under `YAGO_DATA_DIR/LISTS`, honouring the `listname` filter and stripping comment lines. Four requests are admitted before form parsing. Config reads, at most 1,024 owned list names, list reads, and the CRLF response share one 16 MiB budget; overflow or cancellation discards the complete body and returns the existing empty successful response rather than partial policy data. `col=black` is upstream `list.java`'s only implemented column (verified against `source/net/yacy/htroot/yacy/list.java`, 2026-07). |
| Peer message inbox | `/yacy/message.html` | GET, POST | implemented | Serves the YaCy message protocol at full parity (verified against `source/net/yacy/htroot/yacy/message.java`, 2026-07): a network-matched peer whose `youare` addresses this node gets a `permission` grant advertising `messagesize=10240` and `attachmentsize=0`, and a `post` from a peer carrying its `myseed` has its wire-decoded `subject` and `message` body stored in a durable inbox and answered `Thank you!`. Admission measures decoded data and limits the subject to 100 bytes and body to 10,240 bytes. The durable mailbox keeps at most 1,024 records and 8 MiB with deterministic oldest-first eviction and incremental hot-write accounting. Startup trims with key-only pages, rejects oversized values before decoding, removes bounded confirmed-corrupt legacy values after failed decode or validation, and commits progress before continuing. An operational inspection or read failure rolls back the current cleanup page rather than treating unread state as corrupt; earlier pages remain durable, the cleanup cursor stays at its last durable checkpoint, and a pending admission intent remains available for reconciliation. The two behaviors that looked like gaps are upstream's own: message.java hardcodes `attachmentsize=0` (attachments are declined by YaCy too) and comments out the `iam` hash (the sender is taken from the trusted `myseed`), so this is not a narrowing. |
| Peer profile export | `/yacy/profile.html` | GET, POST | implemented | Serves the YaCy profile text shape (`key=value` lines, `\r` stripped and `\n` escaped, empty pairs dropped) from `YAGO_DATA_DIR/SETTINGS/profile.txt` parsed as Java properties. Four requests are admitted before form parsing. The source is limited to 1 MiB, parsing retains at most 1,024 properties and 1 MiB of owned keys and values, and encoding is capped at 2 MiB. Overflow returns the existing empty successful profile. A missing file also yields an empty profile in upstream (`profile.java` swallows the read error, verified against `source/net/yacy/htroot/yacy/profile.java`, 2026-07). |
| Remote crawl URL feed | `/yacy/urls.xml` | GET, POST | partial | Serves the URL-hash metadata feed at parity and returns a well-formed empty delegation feed by default. An opt-in controlled-network policy asynchronously copies eligible locally accepted URL orders into a separate capacity-bounded durable queue without removing the local order. Exact trusted peer hashes authenticated with `salted-magic-sim` may lease at most 100 single-URL items per request inside the clamped 1–20 second budget. Per-peer request rate, outstanding leases, lease TTL, and pending/requeue state are durable. Exact domain or IP-prefix destination policy and DNS answers are checked at staging and lease time. The surface remains partial because delegation is intentionally default-deny and narrower than Java YaCy's unrestricted coupling to its local crawler, not because the wire feed or lease lifecycle is unfinished. |
| Remote crawl receipt | `/yacy/crawlReceipt.html` | POST | partial | The disabled and authentication-rejection path returns YaCy's retry delay `3600`. In enabled mode, only the exact trusted leasing peer may receipt an unexpired matching canonical URL and hash. Receipt values, decoded metadata, and URL length are bounded; destination and DNS policy are checked again before commit. Only `fill` stores the matching URL metadata and removes the lease, returning delay `10`. `unavailable`, `exception`, `robot`, `rejected`, `dequeue`, `update`, `known`, and `stale` requeue and return `3600`; a destination-policy rejection requeues and returns `9999`. Replay, malformed data, wrong peer, wrong URL, or expiry cannot create or extend work. This preserves YaCy's delay vocabulary while keeping remote bodies, profiles, redirects, and follow-up depth outside the delegation contract. |

Peer news travels in the seed `news` property rather than a separate HTTP route.
Decoded records are limited to 1 KiB and require an exact fourteen-digit `cre`
timestamp. Durable duplicate suppression and the incoming, processed, outgoing,
and published queues survive restart. Ordinary categories expire after 24 hours;
profile-update and crawl-start categories expire after 72 hours. Queue retention
is bounded to 4,096 rows and 4 MiB, known identities to 4,096, and out-of-order
intake retains the newest creation times. Cleanup is key/size-first and
progress-making; confirmed corrupt, expired, oversized, orphaned, and
category-mismatched rows are neither served nor published. An operational
inspection or read failure rolls back the current cleanup page instead of
treating unread state as corrupt; earlier committed pages remain durable and
cleanup resumes from its last durable cursor. Admission and rotation intents
remain until replay or rollback and reconciliation complete.

Network-unit interoperability covers the default `freeworld` unit and peers
configured with the same network name. Controlled private networks may select
Java YaCy's `salted-magic-sim` calculation with one nonempty shared secret; the
node signs outbound requests and validates inbound requests through the same
mode. Remote crawl additionally requires its exact trusted-peer and destination
allowlists before the capability flag or work endpoints become active.

## Search Surfaces

All three result surfaces apply `site:` to the exact normalized host and the
equivalent host with one leading `www.` label. Other subdomains are excluded. A
modifier-only request remains keyword-seeded and returns the existing add-word
hint rather than scanning the corpus.

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| YaCy search JSON | `/yacysearch.json` | GET | implemented | Serves local full-text and DHT-selected reachable-peer search results in an upstream-like JSON shape (channel `image`/opensearch fields and the full item shape `title`/`link`/`code`/`description`/`pubDate`/`size`/`sizename`/`guid`/`host`/`path`/`file`/`urlhash`/`ranking`); multi-term remote search uses YaCy index abstracts before secondary URL retrieval, and remote results are ranked with the local ranking profile before the calibrated federated merge (YaCy 1.4 harmonization). An unknown publication date remains an empty `pubDate`; fetch and index time are never substituted. Operational search failures preserve any completed rows as a partial response instead of turning the surface unavailable. A swarm deadline preserves a completed local branch. A classified outer, exact, local-recovery, or provider-incomplete refresh cannot replace an unexpired nonempty search session or extend its TTL; an incomplete global request may use equivalent unexpired local coverage without storing a synthetic global session, while a genuine local miss carrying only peer failures remains an honest empty answer. A `nav=` request returns the `navigation` array (hosts/authors/filetypes/languages/protocols/dates with per-value counts and refine `modifier`/`url`); the filetype navigator and the `filetype:` operator classify a document from its Content-Type with the URL extension as a fallback, so an extension-less PDF or office document (an arxiv `/pdf/<id>`) is matched, not only a `.pdf` URL (ADR-0042); the `author:` operator and the `author=` param steer the author filter; `count` is honored as the OpenSearch alias for `maximumRecords`. Query suggestions are served by the dedicated `/suggest.json` and `/suggest.xml` endpoints rather than embedded in the response, matching upstream; the YaCy-internal `faviconCode` favicon-hash is omitted (this node proxies favicons by host URL). |
| YaCy search RSS | `/yacysearch.rss` | GET | implemented | Serves OpenSearch-flavored RSS from the same local full-text and federated search backend, with per-item Dublin Core `dc:creator`/`dc:publisher`/`dc:subject` from extracted document metadata, the `yacy:size`/`sizename`/`host`/`path`/`file` fields, and the `yacy:navigation` facet element (same navigators, counts, and refine modifiers as the JSON surface) when `nav=` is requested. A swarm deadline preserves a completed local branch; a classified infrastructure- or provider-incomplete refresh reuses an unexpired nonempty session without extending its TTL, and an incomplete global request may use equivalent unexpired local coverage without storing a global session, while a local miss carrying only peer failures remains honest. Image-vertical media enclosures reuse the shared text-first result layout rather than YaCy's per-`contentdom` `media:` elements — the same text-first-node simplification as the HTML surface, not a wire gap. |
| YaCy search HTML | `/yacysearch.html` | GET | implemented | Serves a public search form and result list from the same local full-text and federated search backend, with numbered and prev/next pagination whose links carry every active query parameter (query, resource, contentdom, author, language, filetype, verify, prefer, filter, nav) forward so no filter is dropped between result windows. A `nav=` request renders navigators as collapsible refine links while preserving the other filters. `resource=local` never reaches peers or the web provider. Global search runs exact/morphological local-plus-peer retrieval; a swarm deadline preserves the completed local branch, and a classified infrastructure- or provider-incomplete refresh cannot replace an unexpired nonempty session or extend its TTL. An incomplete global request may use equivalent unexpired local coverage without storing a global session. An empty incomplete exact stage receives one bounded local-exact rescue instead of fuzzy recovery; an honest exact miss receives the bounded fuzzy path. In `always` mode DDGS starts alongside those branches regardless of their hits; otherwise privacy `enabled` continues to DDGS only after the selected local recovery also misses. `explicit` still requires request-level consent. The request path waits at most 50 milliseconds for optional click-impression preparation and persistence. Capacity, a planning timeout, or a persistence error returned within the budget preserves original ordering and direct links without capture metadata; persistence pending at the deadline retains one of four task slots and continues independently until it returns. A click waits for that token's in-flight persistence, and a failed late persistence keeps its token rejected through expiry. Web rows are marked `[web]`. Result rows use one shared text layout across content domains rather than YaCy's per-`contentdom` grids, a deliberate text-first simplification. |
| OpenSearch description | `/opensearchdescription.xml` | GET | implemented | Advertises HTML, RSS, JSON suggestion, and XML suggestion URLs. |
| JSON suggestions | `/suggest.json` | GET | implemented | Serves the OpenSearch suggestion array from the live index — whole matching document titles — merged with recorded recent queries, honouring upstream's full request contract: `count` (clamped to 30), `timeout` (default 300 ms, bounding the index lookup), a validated JSONP `callback`, and open CORS. Deliberate, wire-identical source difference: upstream derives suggestions from a term-dictionary `DidYouMean`; this node returns real indexed titles, which the array shape cannot distinguish. |
| XML suggestions | `/suggest.xml` | GET | implemented | Serves the YaCy-compatible `SearchSuggestion` XML from the same index-title + recent-query source, honouring `count`/`timeout` and setting the open CORS header upstream sends. |
| Solr select compatibility | `/solr/select` | GET, POST | unsupported | Not mounted (upstream also serves `/solr/collection1/select`, `/solr/webgraph/select`, and the two `admin/luke` handlers — none are targets). Solr query compatibility is dropped; local full-text search uses the native Go backend (see `doc/adr/0012-use-bleve-for-embedded-full-text-fallback.md`). |
| GSA search compatibility | `/gsa/searchresult` | GET | unsupported | Not mounted, and no longer a target: upstream removed GSA support on 2020-12-12 ("dropped GSA support"; the servlet survives only in the separate YaCy Grid project), so there is no live surface to be compatible with. |
| MCP and OpenAI-compatible AI surfaces | `/tools*`, `/v1/*`, `/api/tags` | — | unsupported | Deliberate non-goal (operator decision, 2026-07): upstream grew an MCP JSON-RPC search server and OpenAI/Ollama proxy endpoints, but this node's agent surface is the Tavily-compatible `/search`, `/extract`, `/crawl`, and `/map` API — one agent protocol, kept simple. |
| Full embedded Solr API | `/solr/*` | GET, POST | unsupported | Full Solr server compatibility is not a Go peer target. No Solr subset is planned. |

### Swarm remote-search interop (no-Solr divergence)

This node participates in YaCy distributed search over the RWI hash path only; it
never runs Solr/Lucene (ADR-0012). Interop is verified from both directions:

- **We search a real YaCy peer (outbound).** The opt-in end-to-end test
  `TestGlobalSearchFindsRealYaCyResults` (`yagonode/test/e2e/interop_matrix_e2e_test.go`,
  `//go:build e2e`) pushes a document into a live `yacy/yacy_search_server`
  container and confirms our `resource=global` search reaches that peer's
  `/yacy/search.html` with a valid `myseed`, retrieves the URL, and returns the
  hit whose exact query evidence is retained in the transferred URL metadata.
  A received resource is admitted only when its fixed-order 20-column `wi`
  WordReference is present and carries the same URL hash; a malformed response
  is rejected before result or background side effects and counts as invalid
  peer reputation evidence.
- **A real YaCy peer searches us (inbound).**
  `TestRealYaCyGlobalSearchFindsYagoRWI` seeds this node's RWI and URL stores,
  makes a stock Java peer run a global search, and requires it to return the
  Yago-only document. The transient full `wi` row is the Java consumer's
  ranking payload; stored metadata remains unchanged.
- **The response stays parser-compatible without Docker.**
  `TestRemoteSearchWireResponseIsPeerConsumable` (`yagonode/internal/documentsearch`)
  drives `/yacy/search.html` with a multi-word query and parses the raw body with
  the same `yagoproto.ParseSearchResponse` reader used for outbound peer replies.

Multiword outbound primary calls request `abstracts=auto`, reuse those primary
abstracts without exact-term probe calls, and issue at most one stock-compatible
secondary URL retrieval per peer. DHT target reduction retains one eligible
candidate per vertical partition before redundancy, so the default exponent `4`
covers all 16 partitions while the existing 32-attempt and 1.3-second limits
remain unchanged.

The divergence from upstream is that the remote-search answer is built from the
RWI posting index and URL-metadata store, not Solr: our node keeps the YaCy RWI +
URL stores as the peer-exchange/search-interop layer (ADR-0012) while local
public full-text search uses the native Go backend. Solr-only request fields a
current YaCy release may send are accepted without making Solr a runtime
dependency. `filter`, `author`, `filetype`, and `protocol` steer the bounded RWI
result set from stored URL metadata. `prefer`, `profile`, `collection`, and
`timezoneOffset` remain accepted and logged because this path has no trustworthy
equivalent signal. The optional remote-result cache stores validated peer
metadata in the document and full-text layers without overwriting a locally
crawled document. It deliberately does not copy a peer-attached RWI row into the
local exchange vault: the row has no durable sender provenance or expiry and
must not become a locally authoritative DHT claim. Peer `references` remain
untrusted response hints and are not injected into ranking or query expansion;
locally generated response topics remain supported. The live suite defaults to
the pinned stock-Java image
`docker.io/yacy/yacy_search_server@sha256:4225dd07b605347b62ff1fbfa0268217aa79ba2d29bdb0a76d5366d4267398da`;
`YAGO_YACY_IMAGE` can select another explicitly pinned test image.

## Agent API Targets

Every Tavily-compatible JSON request body is limited to 64 KiB. A larger body
returns HTTP 413. Successful responses carry `request_id`; every error body is
exactly `{"detail":{"error":"..."}}` and carries neither a request ID nor a
Yago error object. Authenticated raw-content work across search, extract, crawl,
and map admits four requests process-wide, runs for at most 30 seconds per
request, and limits both retained data and encoded output to 16 MiB.
Admin-minted keys holding the required scope authenticate these endpoints whether scoped-only enforcement is on or off.
`YAGO_SEARCH_REQUIRE_API_KEY` disables only the optional legacy static token;
it does not connect or disconnect the scoped credential authority.
Search domain include/exclude lists use normalized host suffix matching across
local, peer, and web candidates before reciprocal-rank fusion and truncation.
Exclusion wins over inclusion; bounded overfetch and the final response check
prevent a disallowed high-ranked duplicate from consuming an allowed row's slot.

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Tavily-compatible search | `/search` | POST | partial | Defaults to five results. `basic`, `fast`, and `ultra-fast` use local retrieval with `verify=false`; `advanced` uses global local-plus-peer retrieval with `verify=ifexist`. A default result contains `title`, `url`, `content`, `raw_content:null`, and a bounded score that decreases with served rank; it never exposes Yago provenance. `published_date` is news-only and normalized to `YYYY-MM-DD`; favicon and image fields appear only when requested, with URL strings or `{url,description}` entries according to the image-description flag. `max_results` accepts 0 through 20, advanced-only `chunks_per_source` accepts 1 through 3, include/exclude domain lists accept at most 300/150 entries, `country` is validated and general-only, and `safe_search` is rejected for fast depths. With click-capture exposure randomization disabled, equivalent root-portal and Tavily `advanced` requests preserve the same canonical URL order after deduplication. Equivalence requires the same query, parsed filters, false safe-search policy, result limit, and effective web consent. Tavily `basic` and `fast` correspond to the YaCy local surface; the root portal has no local mode. Requested usage is one request-local compatible unit for an executed basic, fast, or ultra-fast search and two for an executed advanced search; an unexecuted `max_results:0` request reports zero. |
| Tavily-compatible extract | `/extract` | POST | partial | Accepts one URL or at most 20 URLs, defaults to basic Markdown extraction, and requires raw scope. `chunks_per_source` accepts 1 through 5 only with a query; request timeout accepts 1 through 60 seconds and defaults to 10 seconds for basic or 30 seconds for advanced. Stored content is preferred, and each stored-document lookup gets at most 250 milliseconds. With operator-enabled fetch-on-extract, an uncached or lookup-timed-out URL uses the remaining request budget through the egress guard and 4 MiB page ceiling. A request deadline, disabled fetch path, or fetch failure becomes a bounded per-URL entry in `failed_results`; completed URLs remain in a mixed HTTP 200 response. Non-timeout storage failures remain endpoint errors. Requested images follow their flag. Requested usage counts complete groups of five successful extractions and doubles them for advanced depth; failed rows do not count. |
| Tavily-compatible crawl | `/crawl` | POST | partial | Performs an authenticated egress-guarded walk with default depth 1, breadth 20, limit 50, and external links allowed. Depth accepts 1 through 5, breadth 1 through 500, and any positive limit is accepted but retention is clipped to YaGo's 200-page cap. Query-like instructions select bounded lexical chunks when `chunks_per_source` is 1 through 5; they do not guide traversal. Crawl accepts a 10-through-150-second timeout but remains subject to YaGo's 30-second hard deadline. Requested usage adds mapping units from complete groups of ten successful pages to extraction units from complete groups of five; instructions double mapping units and advanced depth doubles extraction units. |
| Tavily-compatible map | `/map` | POST | partial | Uses the crawl walk but retains discovered URLs without page text. It shares crawl defaults, bounds, authentication, and the 200-page/30-second YaGo limits. Instructions are accepted but do not alter discovery. Requested usage counts complete groups of ten successful pages and doubles them when instructions are present. |

## Admin And Operations

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Health | `/health` | GET | implemented | Returns a successful status when the ops listener is running. |
| Readiness | `/ready` | GET | implemented | Reports whether local node dependencies are ready to serve traffic, starting with the local search index. |
| Admin authentication | `/api/admin/v1/auth/*` | GET, POST | implemented | First-run admin setup, Argon2id-verified login issuing an HttpOnly `SameSite=Strict` session cookie plus a CSRF token, session introspection, and logout that invalidates the server-side session. Login is rate limited without revealing account existence. Credential JSON and forms are capped at 16 KiB, usernames at 256 bytes, and passwords at 1 KiB; JSON login/setup require `application/json`, the HTML setup form requires its short-lived signed cookie/form token, a shared 32-slot predecode gate bounds unauthenticated login/setup readers, and at most two Argon2 operations run process-wide. An active session rotates to a new unpredictable cookie token after the earlier of one hour or half its configured lifetime; rotation preserves the CSRF token and absolute expiry, atomically invalidates the old token, and fails closed if the replacement cannot be persisted. |
| Admin API keys | `/api/admin/v1/auth/api-keys` | GET, POST, DELETE | implemented | Mints high-entropy API keys with explicit scopes, returns the secret once, stores only its SHA-256 hash with a public identifier, lists key metadata and last-used time without the secret, and revokes keys by identifier. A `Authorization: Bearer <key>` request authenticates operations as an alternative to the session cookie and always authenticates the Tavily-compatible public API when its search scope matches; scoped-only mode disables only the legacy static search token. Keys are rate limited per key and need no CSRF token. Lookup and post-limit last-used persistence share a 32-slot process-wide nonblocking gate; saturation or store failure returns `503` with `Retry-After`, and throttling returns `429` with `Retry-After`. |
| Metrics | `/metrics` | GET | implemented | Serves Prometheus metrics for node operations. Requires a valid admin session or an API key with the `admin:read` scope. |
| Recent events | `/api/admin/v1/events` | GET | implemented | Serves recent structured node events newest-first from a bounded in-memory ring, each with a UTC time, severity, category, name, and message; an optional `limit` bounds the count and a non-positive or non-numeric `limit` is rejected with `400`. A bounded asynchronous queue persists events without blocking request or crawl-progress paths. Shutdown drains admitted writes for up to five seconds, cancels the worker on expiry, and grants five more seconds for quiescence. A writer that still ignores cancellation is reported as an error; vault close is skipped rather than racing its transaction, and the next startup uses scan-based recovery. Recent events survive a clean restart. Requires a valid admin session or an API key with the `admin:read` scope. |
| DHT gate report | `/api/admin/v1/network/dht/gates` | GET | implemented | Serves outbound DHT gate state, configuration, and gate results. Public reachability is reported separately as reachable/unreachable/unconfirmed with its evidence source and observation time; matching YaCy, it is not an outbound sender gate. Requires a valid admin session or an API key with the `admin:read` scope. |
| Search index stats | `/api/admin/v1/index/stats` | GET | implemented | Serves local search backend availability, backend name, indexed document count, and last index update time. Requires a valid admin session or an API key with the `admin:read` scope. |
| Search ranking explain | `/api/admin/v1/search/explain` | POST | implemented | Explains bounded local or global search. Local scope accepts optional preview weights. Global scope shares the active local, peer, web, recovery, analyzer-evidence, and reciprocal-rank fusion path, then returns human-facing provenance, retrieval and final scores, structured fusion contributions, partial failures, learned signal/tree contributions, model revision, and final ranks without saving a paging session, remote result, or crawl seed. Requires a valid admin session with a CSRF token, or an API key with the `search:read` scope. |
| Search ranking trusted domains | `/api/admin/v1/search/ranking/trust` | GET, PUT | implemented | Reads or atomically replaces the persistent operator-curated host trust policy used by domain authority. The policy accepts a blend from 0 through 1 and at most 256 canonical domains or IP hosts; replacing it triggers an immediate authority refresh. GET requires a valid admin session or an API key with the `admin:read` scope. PUT requires a valid admin session with a CSRF token, or an API key with the `admin:write` scope. |
| Crawl dispatch | `/crawl` | POST | partial | Publishes local crawl orders only when crawler integration is configured; supports `startMode` values `url`, `sitemap`, `sitelist`, and `robots`; validates the crawl profile before publishing, rejecting an impossible URL regex or an out-of-range depth, pages-per-host, or duration with `400`. Requires a valid admin session with a CSRF token, or an API key with the `crawl:write` scope. |
| Compatibility report | `/api/admin/v1/compatibility` | GET | implemented | Serves the machine-readable compatibility catalog. Requires a valid admin session or an API key with the `admin:read` scope. |
| Java YaCy admin page clone set | `/*_p.html` | GET, POST | unsupported | Java YaCy administration pages are not cloned into the Go peer. The auth model diverges deliberately (audited against `yacy/yacy_search_server`, 2026-07): upstream protects its `_p` admin servlets with HTTP digest auth, a localhost bypass, per-user UserDB accounts, and a `serverClient` IP allowlist, and has no API-key mechanism at all; this node instead uses Argon2id session login plus scoped bearer API keys on a dedicated ops listener. YaCy admin tooling (digest clients, `apicall.sh`, localhost-implicit scripts) therefore does not work against this node, while plain YaCy *search* clients are unaffected — both sides serve `/yacysearch.*` publicly by default. |

Extraction generation is an additive JSON member inside the existing crawler
ingest protobuf envelope. Generation `0` remains the missing-field legacy value,
the current generation is `1`, older JSON readers ignore the member, and no YaCy
peer wire shape changes. Admin → Index can explicitly traverse a bounded portion
of stored documents and dispatch older generations for ordinary durable crawling;
that Yago-local operator action is not a Java YaCy administration endpoint and
does not change the compatibility status totals above.

Every operations endpoint except `/health` and `/ready` requires a valid admin
session or an API key holding the scope the path needs; cookie-authenticated
unsafe methods additionally require the CSRF token returned at login, while a
Bearer API key needs none. Provision the administrator with `YAGO_ADMIN_USER` and
`YAGO_ADMIN_PASSWORD`, or `POST /api/admin/v1/auth/setup` on first run. There is no
default password. JSON setup and login requests use `Content-Type:
application/json`. API keys are created with `POST /api/admin/v1/auth/api-keys`, are
shown only once, and carry scopes `admin:read`, `admin:write`, `crawl:write`,
`search:read`, or `search:raw`.

Cross-origin browser requests are denied by default. Allowlist origins for the
operations surface with `YAGO_ADMIN_CORS_ORIGINS` and for the public search
endpoints with `YAGO_SEARCH_CORS_ORIGINS`; requests without an `Origin` header,
including all `/yacy/*` peer traffic, are unaffected. The operations listener
(`YAGO_OPS_ADDR`) and the peer listener (`YAGO_PEER_ADDR`) bind separately, so the
admin surface can be kept on loopback behind a reverse proxy while P2P stays
public.

Full Java YaCy page parity, GSA compatibility, model-generated answers,
semantic chunk reranking, model-guided crawl, image ranking/search, and an
optional upstream Tavily provider are not implemented. Tavily-compatible usage
is request-local accounting, not billing, an account balance,
external-provider spend, or evidence of an upstream Tavily call.
`auto_parameters` normalizes reported topic/depth without intent inference;
`country` does not add a geographic boost, `finance` has no dedicated vertical,
and basic, fast, and ultra-fast currently share one local retrieval plan.
