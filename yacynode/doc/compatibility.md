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
| Host-link index | `/yacy/idx.json` | GET, POST | partial | Serves the host object shape with a bounded incoming host-link index inferred from stored URL metadata referrers. |
| Shared blacklist export | `/yacy/list.html` | GET, POST | partial | Checks the YaCy network unit and serves `col=black` from files named in `YACY_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`, under `YACY_DATA_DIR/LISTS`. |
| Peer message inbox | `/yacy/message.html` | GET, POST | partial | Accepts permission checks without requiring `iam` or parsing post-only body fields and stores inbound message posts; attachments are not stored. |
| Peer profile export | `/yacy/profile.html` | GET, POST | partial | Serves profile properties from `YACY_DATA_DIR/SETTINGS/profile.txt` when that YaCy-compatible file exists. |
| Remote crawl URL feed | `/yacy/urls.xml` | GET, POST | partial | Serves URL-hash metadata feeds and safe empty remote-crawl feeds. |
| Remote crawl receipt | `/yacy/crawlReceipt.html` | POST | partial | Accepts the wire shape, returns no delay field on network-auth failure, and returns YaCy's rejected-receipt retry delay for same-network malformed or wrong target hashes while remote crawl execution is disabled. |

## Search Surfaces

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| YaCy search JSON | `/yacysearch.json` | GET | partial | Serves local full-text and DHT-selected reachable-peer search results in an upstream-like JSON shape; multi-term remote search uses YaCy index abstracts before secondary URL retrieval. |
| YaCy search RSS | `/yacysearch.rss` | GET | partial | Serves OpenSearch-flavored RSS from the same local full-text and federated search backend. |
| YaCy search HTML | `/yacysearch.html` | GET | partial | Serves a simple public search form and result list from the same local full-text and federated search backend. |
| OpenSearch description | `/opensearchdescription.xml` | GET | implemented | Advertises HTML, RSS, JSON suggestion, and XML suggestion URLs. |
| JSON suggestions | `/suggest.json` | GET | partial | Serves suggestions from bounded in-memory recent queries. |
| XML suggestions | `/suggest.xml` | GET | partial | Serves YaCy-compatible `SearchSuggestion` XML from the same recent-query source. |
| Solr select compatibility | `/solr/select` | GET, POST | planned | Not mounted. Full Solr compatibility is not claimed. |
| GSA search compatibility | `/gsa/searchresult` | GET | planned | Not mounted. |
| Full embedded Solr API | `/solr/*` | GET, POST | unsupported | Full Solr server compatibility is not a Go peer target. Only a bounded `/solr/select` subset is planned. |

## Agent API Targets

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Tavily-compatible search | `/search` | POST | partial | Serves a Tavily-like response over the shared search core; accepts current search contract fields, ignores unknown fields for forward compatibility, optionally requires `Authorization: Bearer <YAGO_SEARCH_API_KEY>`, returns request IDs and JSON error envelopes, uses local full-text search for basic/fast depths, and includes DHT-selected reachable peer search for `search_depth=advanced`. |
| Tavily-compatible extract | `/extract` | POST | planned | Not mounted. |

## Admin And Operations

| Surface | Path | Methods | Status | Behavior |
| --- | --- | --- | --- | --- |
| Health | `/health` | GET | implemented | Returns a successful status when the ops listener is running. |
| Readiness | `/ready` | GET | implemented | Reports whether local node dependencies are ready to serve traffic, starting with the local search index. |
| Metrics | `/metrics` | GET | implemented | Serves Prometheus metrics for node operations. |
| DHT gate report | `/api/admin/v1/network/dht/gates` | GET | implemented | Serves outbound DHT gate state, configuration, and gate results. |
| Search index stats | `/api/admin/v1/index/stats` | GET | implemented | Serves local search backend availability, backend name, indexed document count, and last index update time. |
| Crawl dispatch | `/api/admin/v1/crawl/jobs` | POST | partial | Publishes local crawl orders only when crawler integration is configured. |
| Compatibility report | `/api/admin/v1/compatibility` | GET | implemented | Serves the machine-readable compatibility catalog. |
| Java YaCy admin page clone set | `/*_p.html` | GET, POST | unsupported | Java YaCy administration pages are not cloned into the Go peer. |

Admin authentication, Carbon UI pages, richer admin APIs, full Java YaCy page
parity, Solr/GSA compatibility, Tavily answer generation, image search, real
usage accounting, hashed API key storage, scopes, rate limits, optional upstream
Tavily, and Tavily-compatible extract APIs remain planned work.
