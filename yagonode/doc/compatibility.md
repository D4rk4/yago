# Compatibility Status

This project is a Go YaCy-compatible peer in progress. It does not claim full
Java YaCy Search Server compatibility. Compatibility is implemented and verified
surface by surface.

As of 2026-07 the surface-by-surface review is complete: every mounted surface is
either `implemented` (served with tests) or carries a recorded `partial` /
`unsupported` decision — none is unfinished work. The surfaces that remain
`partial` are narrower by deliberate scoping, not pending: remote crawl execution
is disabled for SSRF safety (so `/yacy/crawlReceipt.html` and the `/yacy/urls.xml`
delegation feed stay minimal), live fetch-on-extract is operator-controlled and
disabled by default, Tavily answers are deterministic extraction rather than
model generation, and admin crawl dispatch depends on crawler integration being
configured.

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

## YaCy Peer Protocol

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Peer liveness handshake | `/yacy/hello.html` | GET, POST | implemented | Returns caller IP, caller peer type, own seed, and a bounded known seed list after rejecting self-pings and callers using this peer hash. |
| RWI and URL count query | `/yacy/query.html` | GET, POST | implemented | Answers YaCy-compatible `rwicount`, per-word `rwiurlcount`, `lurlcount`, and zero-valued `wanted*` probes with target identity checks. |
| Inbound RWI transfer | `/yacy/transferRWI.html` | POST | implemented | Checks the YaCy network unit and required transfer fields before intake, refuses with `not_granted` when the operator turned the accept-remote-index capability off (allowReceiveIndex parity), sheds intake with YaCy's "too high load" answer when all admission slots are busy, accepts RWI postings durably (batch-size and storage-capacity pressure answer busy with a pause), and reports missing URL metadata. |
| Inbound URL metadata transfer | `/yacy/transferURL.html` | POST | implemented | Checks the YaCy network unit before target handling, refuses with `error_not_granted` when the operator turned the accept-remote-index capability off (allowReceiveIndex parity), sheds intake with the endpoint's not-granted answer when all admission slots are busy, accepts URL metadata, and reconciles RWI references. |
| Remote RWI search | `/yacy/search.html` | GET, POST | implemented | Serves key-value YaCy remote search responses from local RWI storage (never Solr — see the swarm interop note below), clamps requested count to 10 and time to 3,000 milliseconds like YaCy, answers per-term `indexcount`/`indexabstract` when a peer requests abstracts (the multi-term index-abstract negotiation), and sheds concurrent floods with empty-but-valid responses. Its own search deadline also produces a parseable HTTP 200 empty answer with measured `searchtime` and no partial counts or abstracts, avoiding the HTTP 500 that would make a Java YaCy caller drop the interface; caller-owned cancellation remains an error. Outbound searches carry the node's current compact seed in `myseed`, as required by current YaCy peers. A wire-conformance test drives this route and feeds the raw body back through the same peer-response parser the outbound path uses, proving the output is consumable by a YaCy-compatible peer. |
| Seed list | `/yacy/seedlist.html` | GET, POST | implemented | Serves own and confirmed reachable seeds in plain seed-list form with YaCy request filters — `my` (own seed only, YaCy containsKey semantics), `id`/`name`/`peername` single-seed selection, `node`, `me`, `minversion`, `maxcount`; configured bootstrap import accepts seed `UTC` offset and timestamp wire values. Inbound seeds are limited to 32 KiB, 128 properties, 128-byte keys, 8 KiB generic/news values, and a 256-byte name. Bootstrap retains at most 4,096 seeds/16 MiB, and search target selection reuses an owned 4,096-peer/16 MiB mutation-invalidated roster snapshot. |
| Seed list JSON | `/yacy/seedlist.json` | GET, POST | implemented | Serves own and confirmed reachable seeds in JSON seed-list form with the same YaCy request filters as the plain seed list. |
| Seed list XML | `/yacy/seedlist.xml` | GET, POST | implemented | Serves own and confirmed reachable seeds in XML seed-list form with the same YaCy request filters as the plain seed list. |
| Bootstrap seeds | `/p2p/seeds` | GET, POST | implemented | Serves the plain CRLF seed-string list at upstream's unauthenticated bootstrap path (the same list principal peers upload to a bootstrap position), with the shared seedlist filters (`maxcount` capped at 1000, `minversion`, `node`, `me`, `address`, `my`, `id`, `name`, `peername`). |
| Bootstrap seeds JSON | `/p2p/seeds.json` | GET, POST | implemented | Serves the peers-array JSON bootstrap shape (hash-first seed maps plus public `Address` entries, JSONP `callback` supported) from the same backend as `/yacy/seedlist.json`. |
| Host-link index | `/yacy/idx.json` | GET, POST | implemented | Serves the `object=host` shape with an incoming host-link index counted from stored document outlinks per source host, advertising the exact `String h-6, Cardinal m-4 {b256}, Cardinal c-4 {b256}` rowdef and emitting each reference in YaCy's `toPropertyForm(':')` shape. The completion-relative background corpus pass stops at 4,096 target hosts, 64 source hosts per target, and 32,768 total references before further graph entries are allocated, persists the graph with the other corpus signals, and publishes an immutable snapshot. Endpoint intake admits four requests, and each request only reads that snapshot; peer traffic never starts or waits for a document-store scan. `object=host` is upstream `idx.java`'s only implemented object (verified against `source/net/yacy/htroot/yacy/idx.java` + `WebStructureGraph.java`, 2026-07). |
| Shared blacklist export | `/yacy/list.html` | GET, POST | implemented | Checks the YaCy network unit and serves `col=black` from files named in `YAGO_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`, under `YAGO_DATA_DIR/LISTS`, honouring the `listname` filter and stripping comment lines. Four requests are admitted before form parsing. Config reads, at most 1,024 owned list names, list reads, and the CRLF response share one 16 MiB budget; overflow or cancellation discards the complete body and returns the existing empty successful response rather than partial policy data. `col=black` is upstream `list.java`'s only implemented column (verified against `source/net/yacy/htroot/yacy/list.java`, 2026-07). |
| Peer message inbox | `/yacy/message.html` | GET, POST | implemented | Serves the YaCy message protocol at full parity (verified against `source/net/yacy/htroot/yacy/message.java`, 2026-07): a network-matched peer whose `youare` addresses this node gets a `permission` grant advertising `messagesize=10240` and `attachmentsize=0`, and a `post` from a peer carrying its `myseed` has its wire-decoded `subject` and `message` body stored in a durable inbox and answered `Thank you!`. The two behaviors that looked like gaps are upstream's own: message.java hardcodes `attachmentsize=0` (attachments are declined by YaCy too) and comments out the `iam` hash (the sender is taken from the trusted `myseed`), so this is not a narrowing. |
| Peer profile export | `/yacy/profile.html` | GET, POST | implemented | Serves the YaCy profile text shape (`key=value` lines, `\r` stripped and `\n` escaped, empty pairs dropped) from `YAGO_DATA_DIR/SETTINGS/profile.txt` parsed as Java properties. Four requests are admitted before form parsing. The source is limited to 1 MiB, parsing retains at most 1,024 properties and 1 MiB of owned keys and values, and encoding is capped at 2 MiB. Overflow returns the existing empty successful profile. A missing file also yields an empty profile in upstream (`profile.java` swallows the read error, verified against `source/net/yacy/htroot/yacy/profile.java`, 2026-07). |
| Remote crawl URL feed | `/yacy/urls.xml` | GET, POST | partial | Serves the URL-hash metadata feed (resolving requested hashes to stored URL metadata) at parity, and answers the remote-crawl delegation feed with a well-formed empty document. The empty delegation feed is a deliberate consequence of the node not delegating crawl work to remote peers — the same remote-crawl scoping that keeps `/yacy/crawlReceipt.html` narrower — so bringing it to full parity is out of scope while remote crawl stays disabled, not unfinished work. |
| Remote crawl receipt | `/yacy/crawlReceipt.html` | POST | partial | Accepts the wire shape and answers every rejection with YaCy's retry delay of 3600 — including a network-auth failure, matching upstream, which sets delay=3600 before the auth return (verified against `source/net/yacy/htroot/yacy/crawlReceipt.java`, 2026-07) — while remote crawl execution is disabled. Upstream's full delay matrix (9999 for Robinson/domain/blacklist rejections, 10 on a successful `fill` with fulltext store and delegated-URL cleanup) applies only to enabled remote crawl and stays out of scope with it. |

## Search Surfaces

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
- **A YaCy peer searches us (inbound).** `TestRemoteSearchWireResponseIsPeerConsumable`
  (`yagonode/internal/documentsearch`) drives our real `/yacy/search.html` route
  with a multi-word query and parses the raw wire body with the same
  `yagoproto.ParseSearchResponse` reader used to consume other peers — asserting a
  YaCy-compatible peer can parse our `searchtime`/`references`/`joincount`/`count`/
  `resourceN`/`indexcount.<hash>`/`indexabstract.<hash>` response. This is
  deterministic and runs in CI, so it guards the wire contract even where a live
  YaCy container is unavailable.

The divergence from upstream is that the remote-search answer is built from the
RWI posting index and URL-metadata store, not Solr: our node keeps the YaCy RWI +
URL stores as the peer-exchange/search-interop layer (ADR-0012) while local
public full-text search uses the native Go backend. Solr-only request fields a
current YaCy release may send (`prefer`, `filter`, `profile`, `author`,
`collection`, `filetype`, `protocol`, `timezoneOffset`) are accepted and logged
but do not steer the RWI search, so an interoperating peer's request never fails
on them. To exercise the inbound direction against a live YaCy peer manually, run
the `//go:build e2e` suite (it pulls `docker.io/yacy/yacy_search_server:latest`),
which CI does not run because forcing an external peer's DHT to target this node
for a given query hash is non-deterministic.

## Agent API Targets

Every Tavily-compatible JSON request body is limited to 64 KiB. A larger body
returns HTTP 413. Successful responses carry `request_id`; every error body is
exactly `{"detail":{"error":"..."}}` and carries neither a request ID nor a
Yago error object. Authenticated raw-content work across search, extract, crawl,
and map admits four requests process-wide, runs for at most 30 seconds per
request, and limits both retained data and encoded output to 16 MiB.

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Tavily-compatible search | `/search` | POST | partial | Defaults to five results. `basic`, `fast`, and `ultra-fast` use local retrieval with `verify=false`; `advanced` uses global local-plus-peer retrieval with `verify=ifexist`. A default result contains `title`, `url`, `content`, `raw_content:null`, and a bounded score that decreases with served rank; it never exposes Yago provenance. `published_date` is news-only and normalized to `YYYY-MM-DD`; favicon and image fields appear only when requested, with URL strings or `{url,description}` entries according to the image-description flag. `max_results` accepts 0 through 20, advanced-only `chunks_per_source` accepts 1 through 3, include/exclude domain lists accept at most 300/150 entries, `country` is validated and general-only, and `safe_search` is rejected for fast depths. With click-capture exposure randomization disabled, equivalent root-portal and Tavily `advanced` requests preserve the same canonical URL order after deduplication. Equivalence requires the same query, parsed filters, false safe-search policy, result limit, and effective web consent. Tavily `basic` and `fast` correspond to the YaCy local surface; the root portal has no local mode. |
| Tavily-compatible extract | `/extract` | POST | partial | Accepts one URL or at most 20 URLs, defaults to basic Markdown extraction, and requires raw scope. `chunks_per_source` accepts 1 through 5 only with a query; request timeout accepts 1 through 60 seconds and defaults to 10 seconds for basic or 30 seconds for advanced. Stored content is preferred. Operator-enabled live misses use the egress guard, 4 MiB page ceiling, and bounded failure rows. Requested images and local synthetic `{credits:0}` usage follow their flags. |
| Tavily-compatible crawl | `/crawl` | POST | partial | Performs an authenticated egress-guarded walk with default depth 1, breadth 20, limit 50, and external links allowed. Depth accepts 1 through 5, breadth 1 through 500, and any positive limit is accepted but retention is clipped to YaGo's 200-page cap. Query-like instructions select bounded lexical chunks when `chunks_per_source` is 1 through 5; they do not guide traversal. Crawl accepts a 10-through-150-second timeout but remains subject to YaGo's 30-second hard deadline. |
| Tavily-compatible map | `/map` | POST | partial | Uses the crawl walk but retains discovered URLs without page text. It shares crawl defaults, bounds, authentication, and the 200-page/30-second YaGo limits. Instructions are accepted but do not alter discovery. |

## Admin And Operations

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Health | `/health` | GET | implemented | Returns a successful status when the ops listener is running. |
| Readiness | `/ready` | GET | implemented | Reports whether local node dependencies are ready to serve traffic, starting with the local search index. |
| Admin authentication | `/api/admin/v1/auth/*` | GET, POST | implemented | First-run admin setup, Argon2id-verified login issuing an HttpOnly `SameSite=Strict` session cookie plus a CSRF token, session introspection, and logout that invalidates the server-side session. Login is rate limited without revealing account existence. Credential JSON and forms are capped at 16 KiB, usernames at 256 bytes, and passwords at 1 KiB; JSON login/setup require `application/json`, the HTML setup form requires its short-lived signed cookie/form token, a shared 32-slot predecode gate bounds unauthenticated login/setup readers, and at most two Argon2 operations run process-wide. |
| Admin API keys | `/api/admin/v1/auth/api-keys` | GET, POST, DELETE | implemented | Mints high-entropy API keys with explicit scopes, returns the secret once, stores only its SHA-256 hash with a public identifier, lists key metadata and last-used time without the secret, and revokes keys by identifier. A `Authorization: Bearer <key>` request authenticates operations as an alternative to the session cookie, is checked against the scope the path needs, is rate limited per key, and needs no CSRF token. Lookup and post-limit last-used persistence share a 32-slot process-wide nonblocking gate; saturation returns `503` with `Retry-After`, and throttling returns `429` with `Retry-After`. |
| Metrics | `/metrics` | GET | implemented | Serves Prometheus metrics for node operations. Requires a valid admin session or an API key with the `admin:read` scope. |
| Recent events | `/api/admin/v1/events` | GET | implemented | Serves recent structured node events newest-first from a bounded in-memory ring, each with a UTC time, severity, category, name, and message; an optional `limit` bounds the count and a non-positive or non-numeric `limit` is rejected with `400`. A bounded asynchronous queue persists events without blocking request or crawl-progress paths. Shutdown drains admitted writes for up to five seconds; on expiry service shutdown proceeds, while storage close waits for writer quiescence. Recent events survive restart. Requires a valid admin session or an API key with the `admin:read` scope. |
| DHT gate report | `/api/admin/v1/network/dht/gates` | GET | implemented | Serves outbound DHT gate state, configuration, and gate results. Requires a valid admin session or an API key with the `admin:read` scope. |
| Search index stats | `/api/admin/v1/index/stats` | GET | implemented | Serves local search backend availability, backend name, indexed document count, and last index update time. Requires a valid admin session or an API key with the `admin:read` scope. |
| Search ranking explain | `/api/admin/v1/search/explain` | POST | implemented | Explains bounded local or global search. Local scope accepts optional preview weights. Global scope shares the active local, peer, web, recovery, analyzer-evidence, and reciprocal-rank fusion path, then returns human-facing provenance, retrieval and final scores, structured fusion contributions, partial failures, learned signal/tree contributions, model revision, and final ranks without saving a paging session, remote result, or crawl seed. Requires a valid admin session with a CSRF token, or an API key with the `search:read` scope. |
| Search ranking trusted domains | `/api/admin/v1/search/ranking/trust` | GET, PUT | implemented | Reads or atomically replaces the persistent operator-curated host trust policy used by domain authority. The policy accepts a blend from 0 through 1 and at most 256 canonical domains or IP hosts; replacing it triggers an immediate authority refresh. GET requires a valid admin session or an API key with the `admin:read` scope. PUT requires a valid admin session with a CSRF token, or an API key with the `admin:write` scope. |
| Crawl dispatch | `/crawl` | POST | partial | Publishes local crawl orders only when crawler integration is configured; supports `startMode` values `url`, `sitemap`, and `sitelist`; validates the crawl profile before publishing, rejecting an impossible URL regex or an out-of-range depth, pages-per-host, or duration with `400`. Requires a valid admin session with a CSRF token, or an API key with the `crawl:write` scope. |
| Compatibility report | `/api/admin/v1/compatibility` | GET | implemented | Serves the machine-readable compatibility catalog. Requires a valid admin session or an API key with the `admin:read` scope. |
| Java YaCy admin page clone set | `/*_p.html` | GET, POST | unsupported | Java YaCy administration pages are not cloned into the Go peer. The auth model diverges deliberately (audited against `yacy/yacy_search_server`, 2026-07): upstream protects its `_p` admin servlets with HTTP digest auth, a localhost bypass, per-user UserDB accounts, and a `serverClient` IP allowlist, and has no API-key mechanism at all; this node instead uses Argon2id session login plus scoped bearer API keys on a dedicated ops listener. YaCy admin tooling (digest clients, `apicall.sh`, localhost-implicit scripts) therefore does not work against this node, while plain YaCy *search* clients are unaffected — both sides serve `/yacysearch.*` publicly by default. |

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
semantic chunk reranking, model-guided crawl, image ranking/search, real credit
accounting, and an optional upstream Tavily provider are not implemented.
`auto_parameters` normalizes reported topic/depth without intent inference;
`country` does not add a geographic boost, `finance` has no dedicated vertical,
and basic, fast, and ultra-fast currently share one local retrieval plan.
