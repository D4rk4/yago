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
| Inbound RWI transfer | `/yacy/transferRWI.html` | POST | implemented | Checks the YaCy network unit and required transfer fields before intake, accepts RWI postings durably, and reports missing URL metadata. |
| Inbound URL metadata transfer | `/yacy/transferURL.html` | POST | implemented | Checks the YaCy network unit before target handling, accepts URL metadata, and reconciles RWI references. |
| Remote RWI search | `/yacy/search.html` | GET, POST | implemented | Serves key-value YaCy remote search responses from local RWI storage. |
| Seed list | `/yacy/seedlist.html` | GET, POST | implemented | Serves own and confirmed reachable seeds in plain seed-list form with YaCy request filters, including `minversion`; configured bootstrap import accepts seed `UTC` offset and timestamp wire values. |
| Seed list JSON | `/yacy/seedlist.json` | GET, POST | implemented | Serves own and confirmed reachable seeds in JSON seed-list form with the same YaCy request filters as the plain seed list. |
| Seed list XML | `/yacy/seedlist.xml` | GET, POST | implemented | Serves own and confirmed reachable seeds in XML seed-list form with the same YaCy request filters as the plain seed list. |
| Host-link index | `/yacy/idx.json` | GET, POST | partial | Serves the host object shape with a bounded incoming host-link index counted from stored document outlinks per source host, like the YaCy web structure. |
| Shared blacklist export | `/yacy/list.html` | GET, POST | partial | Checks the YaCy network unit and serves `col=black` from files named in `YAGO_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`, under `YAGO_DATA_DIR/LISTS`. |
| Peer message inbox | `/yacy/message.html` | GET, POST | partial | Accepts permission checks without requiring `iam` or parsing post-only body fields and stores inbound message posts; attachments are not stored. |
| Peer profile export | `/yacy/profile.html` | GET, POST | partial | Serves profile properties from `YAGO_DATA_DIR/SETTINGS/profile.txt` when that YaCy-compatible file exists. |
| Remote crawl URL feed | `/yacy/urls.xml` | GET, POST | partial | Serves URL-hash metadata feeds and safe empty remote-crawl feeds. |
| Remote crawl receipt | `/yacy/crawlReceipt.html` | POST | partial | Accepts the wire shape, returns no delay field on network-auth failure, and returns YaCy's rejected-receipt retry delay for same-network malformed or wrong target hashes while remote crawl execution is disabled. |

## Search Surfaces

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| YaCy search JSON | `/yacysearch.json` | GET | partial | Serves local full-text and DHT-selected reachable-peer search results in an upstream-like JSON shape; multi-term remote search uses YaCy index abstracts before secondary URL retrieval, and remote results are ranked with the local ranking profile before the calibrated federated merge (YaCy 1.4 harmonization). |
| YaCy search RSS | `/yacysearch.rss` | GET | partial | Serves OpenSearch-flavored RSS from the same local full-text and federated search backend. |
| YaCy search HTML | `/yacysearch.html` | GET | partial | Serves a simple public search form and result list from the same local full-text and federated search backend. As a human search surface, it is where DDGS web-search fallback hits (when enabled) show their `[ddgs]` marker, unlike the unmarked Tavily `/search` drop-in. |
| OpenSearch description | `/opensearchdescription.xml` | GET | implemented | Advertises HTML, RSS, JSON suggestion, and XML suggestion URLs. |
| JSON suggestions | `/suggest.json` | GET | partial | Serves suggestions from bounded in-memory recent queries. |
| XML suggestions | `/suggest.xml` | GET | partial | Serves YaCy-compatible `SearchSuggestion` XML from the same recent-query source. |
| Solr select compatibility | `/solr/select` | GET, POST | unsupported | Not mounted. Solr query compatibility is dropped; local full-text search uses the native Go backend (see `doc/adr/0012-use-bleve-for-embedded-full-text-fallback.md`). |
| GSA search compatibility | `/gsa/searchresult` | GET | planned | Not mounted. |
| Full embedded Solr API | `/solr/*` | GET, POST | unsupported | Full Solr server compatibility is not a Go peer target. No Solr subset is planned. |

## Agent API Targets

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Tavily-compatible search | `/search` | POST | partial | Serves a Tavily-like response over the shared search core; accepts current search contract fields, ignores unknown fields for forward compatibility, optionally requires `Authorization: Bearer <YAGO_SEARCH_API_KEY>`, returns request IDs and JSON error envelopes, returns stored page image metadata when `include_images` is requested, uses local full-text search for basic/fast depths, and includes DHT-selected reachable peer search for `search_depth=advanced`. This is a drop-in Tavily surface: responses carry only Tavily-shaped fields and no yago-specific provenance markers. When the optional DDGS web-search fallback is enabled, its results are returned here unmarked (search-only, no page browsing); the `[ddgs]` marker appears only on the human search surfaces. |
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
| Java YaCy admin page clone set | `/*_p.html` | GET, POST | unsupported | Java YaCy administration pages are not cloned into the Go peer. |

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
