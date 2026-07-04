# YagoSeek

**Open search infrastructure for developers.**

*YagoSeek — your own federated search node.*

YagoSeek is a self-hosted, YaCy-compatible peer-to-peer search node: run your own
index, join the federated network, and query it through a Tavily-compatible
Search API or the built-in portal. It is built on `yago`, a Go workspace whose
`yago-node` binary speaks the YaCy RWI/DHT wire protocol, alongside an optional
crawler pipeline.

**YagoSeek** is the product; **`yago`** is the toolkit — the Go module, the
workspace, and the command-line binaries (`yago-node`, `yagocrawler`).

- Project home: https://yagoseek.dev/ — documentation at https://docs.yagoseek.dev/,
  the Search API reference at https://api.yagoseek.dev/, a hosted demo at
  https://demo.yagoseek.dev/, and network status at https://status.yagoseek.dev/.
- Source repository: https://github.com/D4rk4/yago/, importable as the Go module
  `github.com/D4rk4/yago`.

This repository is a fork of https://github.com/nikitakarpei/yacy-rwi-node.
The original author is Nikita Karpei.

## Status

This is alpha-stage software. The current `yago-node` implementation focuses on
YaCy RWI/DHT compatibility, durable document, RWI, and URL metadata vaults,
local and federated search surfaces, and an optional crawler-to-node ingest
path. It is not a drop-in replacement for the Java YaCy Search Server.

The project roadmap in [PLAN.md](PLAN.md) describes a broader target: a practical
self-hosted YaCy-like search peer with P2P participation, crawler integration,
local and federated search, a Tavily-compatible Search API, and an administration
UI. Treat that file as planned direction, not as a claim that every listed
capability is already complete.

## Current Scope

The node currently targets these responsibilities:

- advertise one YaCy senior peer identity;
- answer YaCy peer liveness requests with self-identity rejection and answer RWI
  capacity/status requests, including per-word RWI URL counts;
- serve YaCy seed lists through `/yacy/seedlist.html`, `/yacy/seedlist.json`,
  and `/yacy/seedlist.xml`, including upstream request filters such as
  `minversion`;
- bootstrap peers from configured YaCy seedlists, including seeds with either
  offset or timestamp `UTC` wire values;
- answer YaCy shared blacklist export requests through `/yacy/list.html` with
  YaCy network-unit checks and entries from list files named in
  `YAGO_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`;
- answer YaCy peer profile export requests through `/yacy/profile.html` with
  properties from `YAGO_DATA_DIR/SETTINGS/profile.txt` when that file exists;
- answer YaCy host-link index requests through `/yacy/idx.json?object=host` with
  a bounded incoming host graph counted from stored document outlinks;
- answer YaCy peer message permission requests without requiring `iam` or
  parsing post-only body fields and store inbound `/yacy/message.html` posts
  without attachment support;
- answer YaCy `/yacy/urls.xml` remote crawl URL requests with an empty queue,
  network-check and target-check remote crawl receipts with YaCy retry delay,
  and serve URL-hash metadata requests from locally stored metadata;
- receive inbound RWI postings through `/yacy/transferRWI.html`, including YaCy
  preflight result strings for wrong network units and missing required fields;
- receive URL metadata through `/yacy/transferURL.html` with YaCy network-auth
  preflight behavior;
- run retry-aware outbound DHT handoff cycles fed from stored RWI postings, with
  YaCy-style sender gates, target selection, local deletion, restart recovery,
  restore, peer quarantine, metrics, and a JSON gate status endpoint;
- serve remote RWI search requests through `/yacy/search.html`;
- serve local and DHT-targeted reachable-peer public search requests through
  `/yacysearch.json`, `/yacysearch.rss`, and `/yacysearch.html`, using the
  local full-text `SearchIndex` path for `resource=local`, filtering remote
  targets by advertised RWI inventory, using YaCy index abstracts for multi-term
  remote result conjunctions, and balancing redundant DHT candidates randomly;
- serve Tavily-like `POST /search` responses over the same search core, accepting
  the current search contract fields, returning JSON error envelopes and request
  IDs, optionally requiring a local bearer token, using local search for
  basic/fast depths, using DHT-selected reachable-peer search for
  `search_depth=advanced`, and returning stored page image metadata when
  `include_images` is requested;
- serve Tavily-like `POST /extract` responses that return stored-document content
  for URLs already in the index, with controlled `failed_results` for uncached
  URLs and no fetch-on-extract;
- expose `/opensearchdescription.xml`, `/suggest.json`, and `/suggest.xml` for
  browser search integration and recent-query suggestions;
- store accepted document ingest payloads, RWI postings, and URL metadata
  durably;
- expose `/health`, `/ready`, `/metrics`, DHT gate status, and a recent
  structured event log on the ops listener, including inbound and outbound DHT
  transfer series, peer discovery gauges/counters, local search index stats, and
  a machine-readable compatibility catalog;
- optionally publish `url`, `sitemap`, `sitelist`, or `robots` crawl orders and
  consume crawler ingest batches over gRPC when crawling is configured.

The node stores bounded extracted document text, page description metadata,
bounded image URL/alt metadata, and other document metadata, and maintains an
embedded persistent Bleve full-text fallback index for local public search under
`YAGO_DATA_DIR/search.bleve`. The fallback index is opened on startup and is
rebuilt from the document store only when missing or unusable. Bleve is the
committed local search backend, tuned for web search. The node does not store unbounded raw
HTML bodies. The crawler is a separate, optional worker process that can fetch
pages, build document ingest payloads and YaCy-compatible references, and
publish ingest batches back to the node.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `yagonode` | The `yago-node` daemon, YaCy peer protocol endpoints, document, RWI, and URL metadata vaults, search surfaces, metrics, peer exchange, and node-side crawl orchestration. |
| `yagocrawler` | Optional crawler worker that fetches pages through a bounded HTTP fast path with browser fallback, then emits document ingest payloads, RWI postings, and URL metadata. |
| `yagocrawlcontract` | Shared JSON message contract between the node and crawler. |
| `yagomodel` | YaCy domain values and codecs. |
| `yagoproto` | YaCy peer-to-peer endpoint paths, request/response DTOs, and wire protocol helpers. |
| `yagonode/doc` | User-facing node specification, configuration, protocol, interoperability, and ADR documentation. |
| `FEATURES.md` | Markdown feature catalog for implemented, partial, and planned capabilities. |
| `FORK.md` | Fork goals, compatibility claims, and AGPL and UI legal notices. |
| `PLAN.md` | Development roadmap for the fork. |

## Requirements

- Go 1.26.
- Docker or Podman for container and end-to-end workflows.

Outbound node and crawler connections are screened in-process at dial time, so
no external forward proxy is required. Private networks are blocked by default;
set `YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS=true` (node) or
`YAGOCRAWLER_ALLOW_PRIVATE_NETWORKS=true` (crawler) to open all private space, or
name specific ranges with `YAGO_EGRESS_ALLOW_CIDRS` / `YAGOCRAWLER_ALLOW_CIDRS`
(comma-separated CIDRs) to reach only those private networks. Loopback,
link-local (including the cloud metadata range), and reserved ranges stay blocked
either way.

Build and lint tools are pinned through the repository toolchain flow and are
installed under `.toolchain/` by `make tools` or `make verify`.

## Configuration

The node is configured through environment variables. The minimum required
values for a local node process are:

```sh
YAGO_PEER_HASH=...
YAGO_PEER_NAME=...
```

Generate a peer hash with:

```sh
make peer-hash
```

Common node variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `YAGO_PEER_ADDR` | `:8090` | YaCy peer protocol listener. |
| `YAGO_OPS_ADDR` | `:9090` | Ops listener for `/health`, `/ready`, `/metrics`, and node-side crawl dispatch. Every endpoint except `/health` and `/ready` requires a valid admin session or a scoped `Authorization: Bearer` API key. |
| `YAGO_ADMIN_USER` | empty | Administrator username. When set together with `YAGO_ADMIN_PASSWORD`, the admin is provisioned on every start (authoritative). |
| `YAGO_ADMIN_PASSWORD` | empty | Administrator password, stored as an Argon2id hash. Leave both admin variables empty to instead create the first admin with `POST /api/admin/v1/auth/setup`. There is no default password. |
| `YAGO_ADMIN_CORS_ORIGINS` | empty | Comma-separated origin allowlist for cross-origin browser calls to the ops surface. Empty denies all; cross-origin is off by default. |
| `YAGO_SEARCH_CORS_ORIGINS` | empty | Comma-separated origin allowlist for cross-origin browser calls to the public search endpoints. Empty denies all. |
| `YAGO_DATA_DIR` | `./data` | Directory for persistent node storage, `search.bleve`, YaCy-compatible `SETTINGS/profile.txt`, and shared blacklist files configured by `SETTINGS/yacy.conf`. |
| `YAGO_NETWORK_NAME` | `freeworld` | YaCy network name. |
| `YAGO_ADVERTISE_HOST` | empty | Public host advertised to peers. Required when seedlists are configured. |
| `YAGO_ADVERTISE_PORT` | peer listener port | Public port advertised to peers. |
| `YAGO_PUBLIC_SELF_TEST_URL` | local peer URL | Base URL used by outbound DHT gates to self-test `/yacy/query.html?object=rwicount`. |
| `YAGO_SEEDLIST_URLS` | empty | Comma-separated YaCy seedlist URLs. |
| `YAGO_DHT_REDUNDANCY` | `3` | Redundant DHT targets per vertical partition, matching YaCy freeworld senior peers. |
| `YAGO_DHT_PARTITION_EXPONENT` | `4` | YaCy vertical DHT partition exponent used for outbound transfer and global remote search. |
| `YAGO_STORAGE_QUOTA` | `1GB` | Node storage quota. |
| `YAGO_SEARCH_API_KEY` | empty | Optional local bearer token required by Tavily-compatible `POST /search` when set. |
| `YAGO_CRAWL_RPC_ADDR` | empty | Enables node-crawler integration when set; the address the node serves the crawl gRPC endpoint on (e.g. `:9091`). |

Common crawler variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `YAGOCRAWLER_REQUEST_TIMEOUT` | `15s` | Whole-request deadline for crawler HTTP requests, including body reads. |
| `YAGOCRAWLER_CONNECT_TIMEOUT` | `5s` | Dial timeout for crawler HTTP connections, including DNS and multi-address dialing. |
| `YAGOCRAWLER_TLS_TIMEOUT` | `5s` | TLS handshake timeout for crawler HTTPS requests. |
| `YAGOCRAWLER_HEADER_TIMEOUT` | `10s` | Time allowed for crawler HTTP response headers after the request is written. |
| `YAGOCRAWLER_MAX_REDIRECTS` | `10` | Maximum HTTP redirect hops followed by the crawler fast fetch path. Set `0` to reject the first redirect. |
| `YAGOCRAWLER_SITEMAP_URL_LIMIT` | `10000` | Maximum URLs imported from one sitemap or sitelist crawl seed before frontier admission. |

See [yagonode/doc/configuration.md](yagonode/doc/configuration.md) for the full
configuration reference.

## Local Stack

Copy the examples and fill the required peer values:

```sh
cp .env.example .env
cp docker-compose.yml.example docker-compose.yml
```

Then build and start the stack:

```sh
docker compose up --build
```

The example stack starts:

- `yago-node` on ports `8090` and `9090`;
- `yagocrawler` as the optional crawler worker.

When `YAGO_CRAWL_RPC_ADDR` is configured, the ops listener accepts local crawl dispatch
requests at `POST /crawl`. The request body includes `seeds` and optional
`startMode`; supported modes are `url`, `sitemap`, `sitelist`, and `robots`.
Sitemap and sitelist seeds are fetched by the crawler through the same public-web
egress guards as normal pages and expanded into bounded URL roots before frontier
admission. A `robots` start reads each seed host's `robots.txt` and expands the
sitemaps named in its `Sitemap:` directives.

Useful checks:

```sh
curl -fsS http://127.0.0.1:9090/health
curl -fsS http://127.0.0.1:9090/ready
curl -fsS http://127.0.0.1:9090/metrics
curl -fsS http://127.0.0.1:9090/api/admin/v1/compatibility
curl -fsS http://127.0.0.1:9090/api/admin/v1/network/dht/gates
curl -fsS http://127.0.0.1:9090/api/admin/v1/index/stats
curl -fsS http://127.0.0.1:8090/
curl -fsS 'http://127.0.0.1:8090/yacysearch.json?query=test&resource=local&maximumRecords=10'
curl -fsS 'http://127.0.0.1:8090/yacysearch.json?query=test&resource=global&maximumRecords=10'
curl -fsS 'http://127.0.0.1:8090/yacysearch.rss?query=test&resource=local&maximumRecords=10'
curl -fsS 'http://127.0.0.1:8090/yacysearch.html?query=test&resource=local&maximumRecords=10'
curl -fsS -H 'content-type: application/json' \
  -d '{"query":"test","search_depth":"basic","max_results":5}' \
  http://127.0.0.1:8090/search
curl -fsS http://127.0.0.1:8090/opensearchdescription.xml
curl -fsS 'http://127.0.0.1:8090/suggest.json?query=test'
curl -fsS 'http://127.0.0.1:8090/suggest.xml?query=test'
curl -fsS http://127.0.0.1:8090/yacy/seedlist.html
curl -fsS http://127.0.0.1:8090/yacy/seedlist.json
curl -fsS http://127.0.0.1:8090/yacy/seedlist.xml
curl -fsS http://127.0.0.1:8090/yacy/profile.html
curl -fsS 'http://127.0.0.1:8090/yacy/idx.json?object=host'
curl -fsS 'http://127.0.0.1:8090/yacy/message.html?process=permission'
curl -fsS 'http://127.0.0.1:8090/yacy/list.html?col=black'
curl -fsS 'http://127.0.0.1:8090/yacy/urls.xml?network.unit.name=freeworld&call=remotecrawl'
```

## Development

The repository is a Go workspace with five modules. The main quality gate is:

```sh
make verify
```

That runs formatting checks, `go vet`, linting, architecture checks, tests with
race detection, coverage checks, and builds across all modules.

Focused commands are also available:

```sh
make fmt-check
make vet
make lint
make arch
make test
make cover-check
make build
```

End-to-end tests use container images:

```sh
make e2e-node-image
make e2e-crawler-image
make e2e
```

## Documentation

Start with these documents:

- [FORK.md](FORK.md) for the fork's goals, compatibility claims, and legal
  notices, and [yagonode/doc/fork-roadmap.md](yagonode/doc/fork-roadmap.md) for a
  plain-language roadmap;
- [yagonode/doc/adr/README.md](yagonode/doc/adr/README.md) for the architecture
  decision records and the new-dependency rule;
- [yagonode/doc/specification.md](yagonode/doc/specification.md) for the current
  node contract and non-goals;
- [yagonode/doc/yacy-dht-interop.md](yagonode/doc/yacy-dht-interop.md) for YaCy
  DHT interoperability notes;
- [yagonode/doc/yacy-wire-protocol.md](yagonode/doc/yacy-wire-protocol.md) for
  peer protocol details;
- [yagonode/doc/compatibility.md](yagonode/doc/compatibility.md) for supported,
  partial, planned, and unsupported YaCy/Tavily surfaces;
- [yagonode/doc/yacy-upstream-test-parity.md](yagonode/doc/yacy-upstream-test-parity.md)
  for the mapping between upstream YaCy JUnit tests and this repository's
  compatibility tests;
- [yagonode/doc/remote-crawl-policy.md](yagonode/doc/remote-crawl-policy.md) for
  the disabled-by-default remote crawl security policy;
- [yagocrawler/README.md](yagocrawler/README.md) for crawler behavior;
- [yagocrawlcontract/README.md](yagocrawlcontract/README.md) for node-crawler
  message flow.

## License

This project is licensed under the GNU Affero General Public License version 3.
See [LICENSE](LICENSE).
