# Live sanity sweep — 2026-07-07 (QA-01)

Fresh container from the current image (`docker run --restart unless-stopped`),
wizard completed as a public search node (portal on, web fallback enabled,
static search API key set), then every surface probed over HTTP. Expected
status in parentheses where it is not 200.

## Ops listener (:9090)

| Surface | Result |
|---|---|
| `GET /health`, `GET /ready` (unauthenticated) | 200 |
| `GET /metrics` (admin cookie; deliberately behind the guard) | 200 |
| `GET /admin/{overview,search,autocrawler,crawl,network,index,performance,configuration,security,logs,restart}` (admin cookie) | 200 each |
| Any `/admin/*` without a session (401) | 401 |
| First-run wizard → mandatory restart → login | wizard 200 → node restarted (exit 3, supervisor brought it back) → login 303 |

## Public listener (:8080)

| Surface | Result |
|---|---|
| `GET /` portal, `GET /?q=…`, `GET /?dom=image&q=…` | 200 |
| `GET /yacysearch.{json,rss,html}` | 200 |
| `GET /opensearch.xml`, `GET /suggest.{json,xml}`, `GET /favicon?host=…` | 200 |
| `POST /search` without a key (SEC-02: 401) | 401 |
| `POST /search`, `POST /extract` with the bearer key | 200 |
| `POST /crawl`, `POST /map` with extract-fetch disabled (503 = feature off) | 503 |
| Cyrillic zero-local-result query through the web fallback | 10 results (DDG answered) |

## Peer listener (:8090)

| Surface | Result |
|---|---|
| `GET /` landing | 200 |
| `GET /yacy/seedlist.{json,xml}` | 200 |
| `POST /yacy/hello.html` without peer form fields (400 = wire validation) | 400 |

## Findings

No defects. Two behaviors verified as deliberate: `/metrics` sits behind the
admin guard (operators scrape with a session or reverse-proxy exemption), and
the agent `/crawl`/`/map` endpoints answer 503 while extract-fetch is disabled
rather than pretending to work. Health probes for orchestrators are `/health`
and `/ready` (not `/healthz`/`/readyz`).
