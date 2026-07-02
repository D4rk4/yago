# yago

`yago` is a Go workspace for a lightweight YaCy-compatible Reverse Word Index
node and an optional crawler pipeline.

Project repository: https://github.com/D4rk4/yago/.

This repository is a fork of https://github.com/nikitakarpei/yacy-rwi-node.
The original author is Nikita Karpei.

## Status

This is alpha-stage software. The current implementation focuses on YaCy RWI/DHT
storage and serving, plus an optional crawler-to-node ingest path. It is not a
drop-in replacement for the Java YaCy Search Server.

The project roadmap in [PLAN.md](PLAN.md) describes a broader target: a practical
self-hosted YaCy-like search peer with P2P participation, crawler integration,
local and federated search, a Tavily-compatible Search API, and an administration
UI. Treat that file as planned direction, not as a claim that every listed
capability is already complete.

## Current Scope

The node currently targets these responsibilities:

- advertise one YaCy senior peer identity;
- answer YaCy peer liveness and RWI capacity/status requests;
- serve YaCy seed lists through `/yacy/seedlist.html`, `/yacy/seedlist.json`,
  and `/yacy/seedlist.xml`;
- answer YaCy shared blacklist export requests through `/yacy/list.html` with
  an empty list unless shared blacklists are configured in a future storage
  source;
- answer YaCy peer profile export requests through `/yacy/profile.html` with
  an empty profile unless peer profile fields are configured in a future source;
- answer YaCy host-link index requests through `/yacy/idx.json?object=host` with
  an empty index unless host-link data is configured in a future source;
- answer YaCy peer message permission requests and store inbound
  `/yacy/message.html` posts without attachment support;
- answer YaCy `/yacy/urls.xml` remote crawl URL requests with an empty queue and
  URL-hash metadata requests from locally stored metadata;
- receive inbound RWI postings through `/yacy/transferRWI.html`;
- receive URL metadata through `/yacy/transferURL.html`;
- run retry-aware outbound DHT handoff cycles fed from stored RWI postings, with
  YaCy-style sender gates, target selection, local deletion, restore, and
  metrics;
- serve remote RWI search requests through `/yacy/search.html`;
- store accepted RWI postings and URL metadata durably;
- expose `/health` and `/metrics` on the ops listener;
- optionally publish crawl orders and consume crawler ingest batches over NATS
  JetStream when crawling is configured.

The node deliberately does not store full document bodies. The crawler is a
separate, optional worker process that can fetch pages, build YaCy-compatible
references, and publish ingest batches back to the node.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `yacynode` | The RWI node daemon, peer protocol endpoints, storage, metrics, peer exchange, and node-side crawl orchestration. |
| `yacycrawler` | Optional crawler worker that fetches pages and emits RWI postings plus URL metadata. |
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
  Smokescreen.
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
| `YACY_OPS_ADDR` | `:9090` | Ops listener for `/health`, `/metrics`, and node-side crawl dispatch. |
| `YACY_DATA_DIR` | `./data` | Directory for persistent node storage. |
| `YACY_NETWORK_NAME` | `freeworld` | YaCy network name. |
| `YACY_ADVERTISE_HOST` | empty | Public host advertised to peers. Required when seedlists are configured. |
| `YACY_ADVERTISE_PORT` | peer listener port | Public port advertised to peers. |
| `YACY_SEEDLIST_URLS` | empty | Comma-separated YaCy seedlist URLs. |
| `YACY_STORAGE_QUOTA` | `1GB` | Node storage quota. |
| `NATS_URL` | empty | Enables node-crawler integration when set. |

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
- `yacy-rwi-node` on ports `8090` and `9090`;
- `smokescreen` as the outbound proxy;
- `yacycrawler` as the optional crawler worker.

Useful checks:

```sh
curl -fsS http://127.0.0.1:9090/health
curl -fsS http://127.0.0.1:9090/metrics
curl -fsS http://127.0.0.1:8090/
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
- [yacycrawler/README.md](yacycrawler/README.md) for crawler behavior;
- [yacycrawlcontract/README.md](yacycrawlcontract/README.md) for node-crawler
  message flow.

## License

This project is licensed under the GNU Affero General Public License version 3.
See [LICENSE](LICENSE).
