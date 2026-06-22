# Configuration

The node is configured through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | Log verbosity: `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `YACY_DATA_DIR` | `./data` | Where the node persists its data. |
| `YACY_PEER_ADDR` | `:8090` | Listen address for the YaCy peer protocol. |
| `YACY_OPS_ADDR` | `:9090` | Listen address for the `/health` and `/metrics` endpoints. |
| `YACY_PEER_HASH` | _(required)_ | The 12-character enhanced-Base64 peer hash advertised to the network. |
| `YACY_PEER_NAME` | _(required)_ | Peer name advertised to the network. |
| `YACY_NETWORK_NAME` | `freeworld` | YaCy network to join. Only peers on the same network exchange data. |
| `YACY_SEEDLIST_URLS` | _(empty)_ | Comma-separated YaCy seedlist URLs to discover peers from. |
| `YACY_ADVERTISE_HOST` | _(empty)_ | Public IP or DNS name other peers use to reach you. Required when `YACY_SEEDLIST_URLS` is set. |
| `YACY_ADVERTISE_PORT` | _(the `YACY_PEER_ADDR` port)_ | Port other peers use to reach you. |
| `YACY_ANNOUNCE_INTERVAL` | `10m` | How often to re-announce yourself to the network (e.g. `30s`, `10m`, `1h`). |
| `YACY_TRUSTED_PROXIES` | _(empty)_ | Comma-separated CIDRs or IPs of reverse proxies fronting the node. Set this when running behind a reverse proxy so peers are not told the proxy's address. |
| `YACY_STORAGE_QUOTA` | `1GB` | Storage quota, as a human-readable size (e.g. `512MB`, `1GB`, `20GB`). |

Outbound connections honor the standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` environment variables.
