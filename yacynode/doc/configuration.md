# Configuration

The node is configured through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | Log verbosity: `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `YACY_PEER_BIRTH_DATE` | first start time | Optional peer birth date (`YYYYMMDD`, UTC) stored on the first start with a fresh data directory. Declare it when migrating an established peer identity or when private-network tests need an aged peer; YaCy peers skip DHT targets younger than three days. |
| `YACY_DATA_DIR` | `./data` | Where the node persists its data. New nodes use `yago-node.db`; existing `yacy-rwi.db` files are opened when no `yago-node.db` exists. The embedded full-text fallback index is `search.bleve` inside this directory. The YaCy-compatible peer profile file is `SETTINGS/profile.txt` inside this directory. Shared blacklist export reads `SETTINGS/yacy.conf` `BlackLists.Shared` and the referenced files under `LISTS/`. |
| `YACY_PEER_ADDR` | `:8090` | Listen address for the YaCy peer protocol. |
| `YACY_OPS_ADDR` | `:9090` | Listen address for `/health`, `/ready`, `/metrics`, and ops JSON endpoints. |
| `YACY_PEER_HASH` | _(required)_ | The 12-character enhanced-Base64 peer hash advertised to the network. |
| `YACY_PEER_NAME` | _(required)_ | Peer name advertised to the network. |
| `YACY_NETWORK_NAME` | `freeworld` | YaCy network to join. Only peers on the same network exchange data. |
| `YACY_SEEDLIST_URLS` | _(empty)_ | Comma-separated YaCy seedlist URLs to discover peers from. |
| `YACY_ADVERTISE_HOST` | _(empty)_ | Public IP or DNS name other peers use to reach you. Required when `YACY_SEEDLIST_URLS` is set. |
| `YACY_ADVERTISE_PORT` | _(the `YACY_PEER_ADDR` port)_ | Port other peers use to reach you. |
| `YACY_PUBLIC_SELF_TEST_URL` | local peer URL | Base URL used by outbound DHT gates to self-test `/yacy/query.html?object=rwicount`. Set it to the externally reachable peer URL when the local listener is behind a reverse proxy or NAT. |
| `YACY_ANNOUNCE_INTERVAL` | `10m` | How often to re-announce yourself to the network (e.g. `30s`, `10m`, `1h`). |
| `YACY_GREETS_PER_CYCLE` | `16` | How many peers to greet in each announce cycle. |
| `YACY_NETWORK_DHT` | `true` | Enables the sender-side DHT gate equivalent to YaCy `network.unit.dht`. |
| `YACY_DHT_DISTRIBUTION` | `true` | Enables the sender-side DHT distribution gate equivalent to YaCy `allowDistributeIndex`. |
| `YACY_DHT_ALLOW_WHILE_CRAWLING` | `false` | Allows outbound DHT distribution while local crawling is active. |
| `YACY_DHT_ALLOW_WHILE_INDEXING` | `true` | Allows outbound DHT distribution while local indexing is active. |
| `YACY_DHT_DISTRIBUTION_INTERVAL` | `10s` | How often the outbound DHT scheduler runs a distribution cycle. |
| `YACY_DHT_REDUNDANCY` | `3` | Number of DHT target peers per vertical partition for outbound transfer and global remote search. Accepted values are `1` through `16`; the default matches YaCy freeworld senior peers. |
| `YACY_DHT_PARTITION_EXPONENT` | `4` | YaCy vertical DHT partition exponent. Accepted values are `0` through `8`; the default creates 16 vertical partitions, matching YaCy freeworld. |
| `YACY_DHT_MINIMUM_PEER_AGE_DAYS` | `3` | Minimum peer age for DHT target eligibility. Set `-1` only for controlled tests or private networks that intentionally disable the age gate. |
| `YACY_DHT_MINIMUM_CONNECTED_PEERS` | `33` | Sender-side DHT gate: minimum connected peers before outbound distribution starts. Lower it only for private networks or controlled tests. |
| `YACY_DHT_MINIMUM_RWI_WORDS` | `100` | Sender-side DHT gate: minimum locally stored RWI words before outbound distribution starts. |
| `YACY_TRUSTED_PROXIES` | _(empty)_ | Comma-separated CIDRs or IPs of reverse proxies fronting the node. Set this when running behind a reverse proxy so peers are not told the proxy's address. |
| `YACY_STORAGE_QUOTA` | `1GB` | Storage quota, as a human-readable size (e.g. `512MB`, `1GB`, `20GB`). |
| `YACY_EGRESS_ALLOW_PRIVATE_NETWORKS` | `false` | Allow outbound connections to RFC 1918 and unique-local addresses. Enable only for LAN or private-network deployments; loopback, link-local, and reserved ranges stay blocked. |
| `YAGO_SEARCH_API_KEY` | _(empty)_ | Optional local bearer token for the Tavily-compatible `POST /search` endpoint. When set, callers must send `Authorization: Bearer <token>`. This is not an upstream Tavily API key. |

## Crawling

The node can drive a crawl fleet over gRPC: it serves a `CrawlExchange` endpoint that crawlers dial. Operators start a crawl by posting seed URLs to `/crawl` on the ops address; the node enqueues orders in a durable, store-backed FIFO and streams them to connected crawlers, and crawled pages flow back in as ingest batches. Crawling is off until `YACY_CRAWL_RPC_ADDR` is set; without it the node behaves as a pure peer.

The `/crawl` request body accepts `seeds` and optional `startMode`. Supported
start modes are `url`, `sitemap`, and `sitelist`; empty mode is treated as
`url`. Sitemap and sitelist starts are expanded by the crawler into bounded URL
roots before normal frontier admission.

| Variable | Default | Description |
| --- | --- | --- |
| `YACY_CRAWL_RPC_ADDR` | _(empty)_ | Address the node serves the crawl gRPC endpoint on (e.g. `:9091`). Empty disables crawling. |
