# yago

`yago` is a Go workspace for `yago-node`, a YaCy-compatible P2P search node,
and an optional crawler pipeline.

Project repository: https://github.com/D4rk4/yago/.

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
  `YACY_DATA_DIR/SETTINGS/yacy.conf` `BlackLists.Shared`;
- answer YaCy peer profile export requests through `/yacy/profile.html` with
  properties from `YACY_DATA_DIR/SETTINGS/profile.txt` when that file exists;
- answer YaCy host-link index requests through `/yacy/idx.json?object=host` with
  a bounded incoming host graph inferred from stored URL metadata referrers;
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
  basic/fast depths, and using DHT-selected reachable-peer search for
  `search_depth=advanced`;
- expose `/opensearchdescription.xml`, `/suggest.json`, and `/suggest.xml` for
  browser search integration and recent-query suggestions;
- store accepted document ingest payloads, RWI postings, and URL metadata
  durably;
- expose `/health`, `/ready`, `/metrics`, and DHT gate status on the ops listener,
  including inbound and outbound DHT transfer series, peer discovery
  gauges/counters, local search index stats, and a machine-readable
  compatibility catalog;
- optionally publish crawl orders and consume crawler ingest batches over NATS
  JetStream when crawling is configured.

The node stores bounded extracted document text, page description metadata, and
other document metadata, and maintains an embedded persistent Bleve full-text
fallback index for local public search under
`YACY_DATA_DIR/search.bleve`. The fallback index is opened on startup and is
rebuilt from the document store only when missing or unusable; the Tantivy
production sidecar remains roadmap work. The node does not store unbounded raw
HTML bodies. The crawler is a separate, optional worker process that can fetch
pages, build document ingest payloads and YaCy-compatible references, and
publish ingest batches back to the node.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `yacynode` | The `yago-node` daemon, YaCy peer protocol endpoints, document, RWI, and URL metadata vaults, search surfaces, metrics, peer exchange, and node-side crawl orchestration. |
| `yacycrawler` | Optional crawler worker that fetches pages through a bounded HTTP fast path with browser fallback, then emits document ingest payloads, RWI postings, and URL metadata. |
| `yacycrawlcontract` | Shared JSON message contract between the node and crawler. |
| `yacymodel` | YaCy domain values and codecs. |
| `yacyproto` | YaCy peer-to-peer endpoint paths, request/response DTOs, and wire protocol helpers. |
| `yacynode/doc` | User-facing node specification, configuration, protocol, interoperability, and ADR documentation. |
| `FEATURES.md` | Markdown feature catalog for implemented, partial, and planned capabilities. |
| `PLAN.md` | Development roadmap for the fork. |

## Requirements

- Go 1.26.
- Docker or Podman for container and end-to-end workflows.
- A proxy for outbound node and crawler traffic. The example stack uses
  Smokescreen, and the crawler also rejects non-public destinations before
  fetch.
- NATS JetStream when crawler integration is enabled.

Build and lint tools are pinned through the repository toolchain flow and are
installed under `.toolchain/` by `make tools` or `make verify`.

## Configuration

The node is configured through environment variables. The minimum required
values for a local node process are:

```sh
YACY_PEER_HASH=...
YACY_PEER_NAME=...
YACY_PROXY_URL=http://127.0.0.1:4750
```

Generate a peer hash with:

```sh
make peer-hash
```

Common node variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `YACY_PEER_ADDR` | `:8090` | YaCy peer protocol listener. |
| `YACY_OPS_ADDR` | `:9090` | Ops listener for `/health`, `/ready`, `/metrics`, and node-side crawl dispatch. |
| `YACY_DATA_DIR` | `./data` | Directory for persistent node storage, `search.bleve`, YaCy-compatible `SETTINGS/profile.txt`, and shared blacklist files configured by `SETTINGS/yacy.conf`. |
| `YACY_NETWORK_NAME` | `freeworld` | YaCy network name. |
| `YACY_ADVERTISE_HOST` | empty | Public host advertised to peers. Required when seedlists are configured. |
| `YACY_ADVERTISE_PORT` | peer listener port | Public port advertised to peers. |
| `YACY_PUBLIC_SELF_TEST_URL` | local peer URL | Base URL used by outbound DHT gates to self-test `/yacy/query.html?object=rwicount`. |
| `YACY_SEEDLIST_URLS` | empty | Comma-separated YaCy seedlist URLs. |
| `YACY_DHT_REDUNDANCY` | `3` | Redundant DHT targets per vertical partition, matching YaCy freeworld senior peers. |
| `YACY_DHT_PARTITION_EXPONENT` | `4` | YaCy vertical DHT partition exponent used for outbound transfer and global remote search. |
| `YACY_STORAGE_QUOTA` | `1GB` | Node storage quota. |
| `YAGO_SEARCH_API_KEY` | empty | Optional local bearer token required by Tavily-compatible `POST /search` when set. |
| `NATS_URL` | empty | Enables node-crawler integration when set. |

Common crawler variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `YACYCRAWLER_REQUEST_TIMEOUT` | `15s` | Whole-request deadline for crawler HTTP requests, including body reads. |
| `YACYCRAWLER_CONNECT_TIMEOUT` | `5s` | Dial timeout for crawler HTTP connections, including DNS and multi-address dialing. |
| `YACYCRAWLER_TLS_TIMEOUT` | `5s` | TLS handshake timeout for crawler HTTPS requests. |
| `YACYCRAWLER_HEADER_TIMEOUT` | `10s` | Time allowed for crawler HTTP response headers after the request is written. |
| `YACYCRAWLER_MAX_REDIRECTS` | `10` | Maximum HTTP redirect hops followed by the crawler fast fetch path. Set `0` to reject the first redirect. |

See [yacynode/doc/configuration.md](yacynode/doc/configuration.md) for the full
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

- `nats` with JetStream enabled;
- `yago-node` on ports `8090` and `9090`;
- `smokescreen` as the outbound proxy;
- `yacycrawler` as the optional crawler worker.

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

- [yacynode/doc/specification.md](yacynode/doc/specification.md) for the current
  node contract and non-goals;
- [yacynode/doc/yacy-dht-interop.md](yacynode/doc/yacy-dht-interop.md) for YaCy
  DHT interoperability notes;
- [yacynode/doc/yacy-wire-protocol.md](yacynode/doc/yacy-wire-protocol.md) for
  peer protocol details;
- [yacynode/doc/compatibility.md](yacynode/doc/compatibility.md) for supported,
  partial, planned, and unsupported YaCy/Tavily surfaces;
- [yacynode/doc/remote-crawl-policy.md](yacynode/doc/remote-crawl-policy.md) for
  the disabled-by-default remote crawl security policy;
- [yacycrawler/README.md](yacycrawler/README.md) for crawler behavior;
- [yacycrawlcontract/README.md](yacycrawlcontract/README.md) for node-crawler
  message flow.

## License

This project is licensed under the GNU Affero General Public License version 3.
See [LICENSE](LICENSE).
