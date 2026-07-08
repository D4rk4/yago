# Compatibility Status

This project is a Go YaCy-compatible peer in progress. It does not claim full
Java YaCy Search Server compatibility. Compatibility is implemented and verified
surface by surface.

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
| Inbound RWI transfer | `/yacy/transferRWI.html` | POST | implemented | Checks the YaCy network unit and required transfer fields before intake, sheds intake with YaCy's "too high load" answer when all admission slots are busy, accepts RWI postings durably (batch-size and storage-capacity pressure answer busy with a pause), and reports missing URL metadata. |
| Inbound URL metadata transfer | `/yacy/transferURL.html` | POST | implemented | Checks the YaCy network unit before target handling, sheds intake with the endpoint's not-granted answer when all admission slots are busy, accepts URL metadata, and reconciles RWI references. |
| Remote RWI search | `/yacy/search.html` | GET, POST | implemented | Serves key-value YaCy remote search responses from local RWI storage (never Solr — see the swarm interop note below), clamps requested count and time like YaCy, answers per-term `indexcount`/`indexabstract` when a peer requests abstracts (the multi-term index-abstract negotiation), and sheds concurrent floods with empty-but-valid responses. A wire-conformance test drives this route and feeds the raw body back through the same peer-response parser the outbound path uses, proving the output is consumable by a YaCy-compatible peer. |
| Seed list | `/yacy/seedlist.html` | GET, POST | implemented | Serves own and confirmed reachable seeds in plain seed-list form with YaCy request filters — `my` (own seed only, YaCy containsKey semantics), `id`/`name`/`peername` single-seed selection, `node`, `me`, `minversion`, `maxcount`; configured bootstrap import accepts seed `UTC` offset and timestamp wire values. |
| Seed list JSON | `/yacy/seedlist.json` | GET, POST | implemented | Serves own and confirmed reachable seeds in JSON seed-list form with the same YaCy request filters as the plain seed list. |
| Seed list XML | `/yacy/seedlist.xml` | GET, POST | implemented | Serves own and confirmed reachable seeds in XML seed-list form with the same YaCy request filters as the plain seed list. |
| Bootstrap seeds | `/p2p/seeds` | GET, POST | implemented | Serves the plain CRLF seed-string list at upstream's unauthenticated bootstrap path (the same list principal peers upload to a bootstrap position), with the shared seedlist filters (`maxcount` capped at 1000, `minversion`, `node`, `me`, `address`, `my`, `id`, `name`, `peername`). |
| Bootstrap seeds JSON | `/p2p/seeds.json` | GET, POST | implemented | Serves the peers-array JSON bootstrap shape (hash-first seed maps plus public `Address` entries, JSONP `callback` supported) from the same backend as `/yacy/seedlist.json`. |
| Host-link index | `/yacy/idx.json` | GET, POST | partial | Serves the host object shape with a bounded incoming host-link index counted from stored document outlinks per source host, like the YaCy web structure. |
| Shared blacklist export | `/yacy/list.html` | GET, POST | partial | Checks the YaCy network unit and serves `col=black` from files named in `YAGO_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`, under `YAGO_DATA_DIR/LISTS`. |
| Peer message inbox | `/yacy/message.html` | GET, POST | partial | Accepts permission checks without requiring `iam` or parsing post-only body fields and stores inbound message posts; attachments are not stored. |
| Peer profile export | `/yacy/profile.html` | GET, POST | partial | Serves profile properties from `YAGO_DATA_DIR/SETTINGS/profile.txt` when that YaCy-compatible file exists. |
| Remote crawl URL feed | `/yacy/urls.xml` | GET, POST | partial | Serves URL-hash metadata feeds and safe empty remote-crawl feeds. |
| Remote crawl receipt | `/yacy/crawlReceipt.html` | POST | partial | Accepts the wire shape and answers every rejection with YaCy's retry delay of 3600 — including a network-auth failure, matching upstream, which sets delay=3600 before the auth return (verified against `source/net/yacy/htroot/yacy/crawlReceipt.java`, 2026-07) — while remote crawl execution is disabled. Upstream's full delay matrix (9999 for Robinson/domain/blacklist rejections, 10 on a successful `fill` with fulltext store and delegated-URL cleanup) applies only to enabled remote crawl and stays out of scope with it. |

## Search Surfaces

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| YaCy search JSON | `/yacysearch.json` | GET | partial | Serves local full-text and DHT-selected reachable-peer search results in an upstream-like JSON shape; multi-term remote search uses YaCy index abstracts before secondary URL retrieval, and remote results are ranked with the local ranking profile before the calibrated federated merge (YaCy 1.4 harmonization). A `nav=` request returns the `navigation` array (hosts/authors/filetypes/languages/protocols/dates with per-value counts and refine `modifier`/`url`); the `author:` operator and the `author=` param steer the author filter; `count` is honored as the OpenSearch alias for `maximumRecords`. |
| YaCy search RSS | `/yacysearch.rss` | GET | partial | Serves OpenSearch-flavored RSS from the same local full-text and federated search backend, including the `yacy:navigation` facet element (same navigators, counts, and refine modifiers as the JSON surface) when `nav=` is requested. |
| YaCy search HTML | `/yacysearch.html` | GET | partial | Serves a simple public search form and result list from the same local full-text and federated search backend. As a human search surface, it is where DDGS web-search fallback hits (when enabled) show their `[ddgs]` marker, unlike the unmarked Tavily `/search` drop-in. |
| OpenSearch description | `/opensearchdescription.xml` | GET | implemented | Advertises HTML, RSS, JSON suggestion, and XML suggestion URLs. |
| JSON suggestions | `/suggest.json` | GET | implemented | Serves the OpenSearch suggestion array from the live index — whole matching document titles — merged with recorded recent queries, honouring upstream's full request contract: `count` (clamped to 30), `timeout` (default 300 ms, bounding the index lookup), a validated JSONP `callback`, and open CORS. Deliberate, wire-identical source difference: upstream derives suggestions from a term-dictionary `DidYouMean`; this node returns real indexed titles, which the array shape cannot distinguish. |
| XML suggestions | `/suggest.xml` | GET | implemented | Serves the YaCy-compatible `SearchSuggestion` XML from the same index-title + recent-query source, honouring `count`/`timeout` and setting the open CORS header upstream sends. |
| Solr select compatibility | `/solr/select` | GET, POST | unsupported | Not mounted (upstream also serves `/solr/collection1/select`, `/solr/webgraph/select`, and the two `admin/luke` handlers — none are targets). Solr query compatibility is dropped; local full-text search uses the native Go backend (see `doc/adr/0012-use-bleve-for-embedded-full-text-fallback.md`). |
| GSA search compatibility | `/gsa/searchresult` | GET | unsupported | Not mounted, and no longer a target: upstream removed GSA support on 2020-12-12 ("dropped GSA support"; the servlet survives only in the separate YaCy Grid project), so there is no live surface to be compatible with. |
| MCP and OpenAI-compatible AI surfaces | `/tools*`, `/v1/*`, `/api/tags` | — | unsupported | Deliberate non-goal (operator decision, 2026-07): upstream grew an MCP JSON-RPC search server and OpenAI/Ollama proxy endpoints, but this node's agent surface is the Tavily-compatible `/search`+`/extract` API — one agent protocol, kept simple. |
| Full embedded Solr API | `/solr/*` | GET, POST | unsupported | Full Solr server compatibility is not a Go peer target. No Solr subset is planned. |

### Swarm remote-search interop (no-Solr divergence)

This node participates in YaCy distributed search over the RWI hash path only; it
never runs Solr/Lucene (ADR-0012). Interop is verified from both directions:

- **We search a real YaCy peer (outbound).** The opt-in end-to-end test
  `TestGlobalSearchFindsRealYaCyResults` (`yagonode/test/e2e/interop_matrix_e2e_test.go`,
  `//go:build e2e`) pushes a document into a live `yacy/yacy_search_server`
  container and confirms our `resource=global` search reaches that peer's
  `/yacy/search.html`, negotiates index abstracts, retrieves the URLs, and returns
  the hit.
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

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Tavily-compatible search | `/search` | POST | partial | Serves a Tavily-like response over the shared search core; accepts current search contract fields, ignores unknown fields for forward compatibility, optionally requires `Authorization: Bearer <YAGO_SEARCH_API_KEY>`, returns request IDs and JSON error envelopes, returns stored page image metadata when `include_images` is requested, uses local full-text search for basic/fast depths, and includes DHT-selected reachable peer search for `search_depth=advanced`. The response shape mirrors real Tavily payloads (audited against docs.tavily.com, 2026-07): `answer`, `images`, and `follow_up_questions` are present on every response (null / empty array when not requested), the top-level `images` array holds plain URL strings unless `include_image_descriptions` asks for `{url, description}` objects, and error responses carry Tavily's documented `{"detail": {"error": ...}}` envelope alongside the structured `error` object. Known deltas: `time_range`/`start_date`/`end_date`/`topic`/`country` are accepted but do not steer results, `include_answer` returns an empty string (no local answer synthesis), and `POST /crawl`/`POST /map` are not mounted. This is a drop-in Tavily surface: responses carry only Tavily-shaped fields and no yago-specific provenance markers. When the optional DDGS web-search fallback is enabled, its results are returned here unmarked (search-only, no page browsing); the `[ddgs]` marker appears only on the human search surfaces. |
| Tavily-compatible extract | `/extract` | POST | partial | Returns Tavily-like extract results for URLs already in the document store; accepts `urls` as a string or array plus `extract_depth`, `format`, `include_images`, and `include_favicon`, optionally requires `Authorization: Bearer <YAGO_SEARCH_API_KEY>`, and returns request IDs and JSON error envelopes. Fetch-on-extract is disabled, so URLs absent from the store return controlled `failed_results` entries with no private-network fetch. |

## Admin And Operations

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Health | `/health` | GET | implemented | Returns a successful status when the ops listener is running. |
| Readiness | `/ready` | GET | implemented | Reports whether local node dependencies are ready to serve traffic, starting with the local search index. |
| Admin authentication | `/api/admin/v1/auth/*` | GET, POST | implemented | First-run admin setup, Argon2id-verified login issuing an HttpOnly `SameSite=Strict` session cookie plus a CSRF token, session introspection, and logout that invalidates the server-side session. Login is rate limited and a failed login does not reveal whether the account exists. |
| Admin API keys | `/api/admin/v1/auth/api-keys` | GET, POST, DELETE | implemented | Mints high-entropy API keys with explicit scopes, returns the secret once, stores only its SHA-256 hash with a public identifier, lists key metadata and last-used time without the secret, and revokes keys by identifier. A `Authorization: Bearer <key>` request authenticates operations as an alternative to the session cookie, is checked against the scope the path needs, is rate limited per key, and needs no CSRF token. |
| Metrics | `/metrics` | GET | implemented | Serves Prometheus metrics for node operations. Requires a valid admin session or an API key with the `admin:read` scope. |
| DHT gate report | `/api/admin/v1/network/dht/gates` | GET | implemented | Serves outbound DHT gate state, configuration, and gate results. Requires a valid admin session or an API key with the `admin:read` scope. |
| Search index stats | `/api/admin/v1/index/stats` | GET | implemented | Serves local search backend availability, backend name, indexed document count, and last index update time. Requires a valid admin session or an API key with the `admin:read` scope. |
| Search ranking explain | `/api/admin/v1/search/explain` | POST | implemented | Previews a local search query under caller-supplied ranking weights and returns each result's score and score explanation without saving the weights. Requires a valid admin session with a CSRF token, or an API key with the `search:read` scope. |
| Crawl dispatch | `/crawl` | POST | partial | Publishes local crawl orders only when crawler integration is configured; supports `startMode` values `url`, `sitemap`, and `sitelist`; validates the crawl profile before publishing, rejecting an impossible URL regex or an out-of-range depth, pages-per-host, or duration with `400`. Requires a valid admin session with a CSRF token, or an API key with the `crawl:write` scope. |
| Compatibility report | `/api/admin/v1/compatibility` | GET | implemented | Serves the machine-readable compatibility catalog. Requires a valid admin session or an API key with the `admin:read` scope. |
| Java YaCy admin page clone set | `/*_p.html` | GET, POST | unsupported | Java YaCy administration pages are not cloned into the Go peer. The auth model diverges deliberately (audited against `yacy/yacy_search_server`, 2026-07): upstream protects its `_p` admin servlets with HTTP digest auth, a localhost bypass, per-user UserDB accounts, and a `serverClient` IP allowlist, and has no API-key mechanism at all; this node instead uses Argon2id session login plus scoped bearer API keys on a dedicated ops listener. YaCy admin tooling (digest clients, `apicall.sh`, localhost-implicit scripts) therefore does not work against this node, while plain YaCy *search* clients are unaffected — both sides serve `/yacysearch.*` publicly by default. |

Every operations endpoint except `/health` and `/ready` requires a valid admin
session or an API key holding the scope the path needs; cookie-authenticated
unsafe methods additionally require the CSRF token returned at login, while a
Bearer API key needs none. Provision the administrator with `YAGO_ADMIN_USER` and
`YAGO_ADMIN_PASSWORD`, or `POST /api/admin/v1/auth/setup` on first run. There is no
default password. API keys are created with `POST /api/admin/v1/auth/api-keys`, are
shown only once, and carry scopes `admin:read`, `admin:write`, `crawl:write`,
`search:read`, or `search:raw`.

Cross-origin browser requests are denied by default. Allowlist origins for the
operations surface with `YAGO_ADMIN_CORS_ORIGINS` and for the public search
endpoints with `YAGO_SEARCH_CORS_ORIGINS`; requests without an `Origin` header,
including all `/yacy/*` peer traffic, are unaffected. The operations listener
(`YAGO_OPS_ADDR`) and the peer listener (`YAGO_PEER_ADDR`) bind separately, so the
admin surface can be kept on loopback behind a reverse proxy while P2P stays
public.

Carbon UI pages, richer admin APIs, full Java YaCy page parity, GSA
compatibility, Tavily answer generation, image ranking/search, real usage
accounting, hashed API key storage, scopes, per-key rate limits, optional upstream
Tavily, and fetch-on-extract for uncached URLs remain planned work.
