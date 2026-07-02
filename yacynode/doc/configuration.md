# Configuration

The node is configured through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | Log verbosity: `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `YACY_DATA_DIR` | `./data` | Where the node persists its data. The YaCy-compatible peer profile file is `SETTINGS/profile.txt` inside this directory. |
| `YACY_PEER_ADDR` | `:8090` | Listen address for the YaCy peer protocol. |
| `YACY_OPS_ADDR` | `:9090` | Listen address for `/health`, `/metrics`, and ops JSON endpoints. |
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
| `YACY_TRUSTED_PROXIES` | _(empty)_ | Comma-separated CIDRs or IPs of reverse proxies fronting the node. Set this when running behind a reverse proxy so peers are not told the proxy's address. |
| `YACY_STORAGE_QUOTA` | `1GB` | Storage quota, as a human-readable size (e.g. `512MB`, `1GB`, `20GB`). |
| `YACY_PROXY_URL` | _(required)_ | `http` or `https` URL of the proxy all outbound connections are routed through. |

## Crawling

The node can drive a crawl fleet over NATS JetStream: operators start a crawl by posting seed URLs to `/crawl` on the ops address, and crawled pages flow back in as ingest batches. Crawling is off until `NATS_URL` is set; without it the node behaves as a pure peer.

| Variable | Default | Description |
| --- | --- | --- |
| `NATS_URL` | _(empty)_ | NATS server to reach the crawl fleet (e.g. `nats://nats:4222`). Empty disables crawling. |
| `NATS_ORDERS_SUBJECT` | `yacy.crawl.orders` | Subject crawl orders are published to. Must match the crawler. |
| `NATS_INGEST_SUBJECT` | `yacy.crawl.ingest` | Subject crawled batches arrive on. Must match the crawler. |
| `NATS_INGEST_DURABLE` | `yacy-node` | Durable consumer name for reading ingest batches. |
| `NATS_INGEST_MAX_MSGS` | `1024` | Maximum undelivered ingest batches buffered before the fleet is paused. |
