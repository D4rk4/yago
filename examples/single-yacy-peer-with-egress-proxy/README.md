# Single YaCy peer with egress proxy

Runs one YaCy RWI node as a peer on the YaCy network, joining the DHT and answering remote
search requests. All outbound traffic passes through an egress proxy that blocks requests to
private and internal IP addresses.

This is the smallest deployment: a peer with no crawling and no search UI of its own.

## Run it

1. Copy `.env.example` to `.env` and set `YACY_PEER_HASH`, `YACY_PEER_NAME`, and
   `YACY_ADVERTISE_HOST`.
2. Copy `docker-compose.yml.example` to `docker-compose.yml`.
3. Start the stack: `docker compose up -d --build`.

## What's running

| Service | Role |
| --- | --- |
| `yacy-rwi-node` | The peer: joins the DHT, serves remote search requests, exposes `/health` and `/metrics`. |
| `smokescreen` | Egress proxy every outbound connection from the node passes through. |

## Ports

| Port | Purpose |
| --- | --- |
| 8090 | YaCy peer protocol |
| 9090 | `/health` and `/metrics` |

See `yacynode/doc/configuration.md` for every environment variable the node accepts.
