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
| `YACY_EGRESS_ALLOW_CIDRS` | _(empty)_ | Comma-separated private CIDRs the egress guard admits even when `YACY_EGRESS_ALLOW_PRIVATE_NETWORKS` is false, so intranet mode reaches only named ranges instead of all private space. Only relaxes the private check: loopback, link-local (including the cloud metadata range), and reserved ranges stay blocked, so a non-private entry never grants access to them. |
| `YAGO_SEARCH_API_KEY` | _(empty)_ | Optional local bearer token for the Tavily-compatible `POST /search` endpoint. When set, callers must send `Authorization: Bearer <token>`. This is a local key for the node's own endpoint, not a key for any external search service; the node uses no keyed external search API. |
| `YAGO_ADMIN_USER` | _(empty)_ | Administrator username. When set with `YAGO_ADMIN_PASSWORD`, the admin is provisioned on every start and those credentials are authoritative. |
| `YAGO_ADMIN_PASSWORD` | _(empty)_ | Administrator password, stored as an Argon2id hash. Leave both admin variables empty to create the first admin with `POST /api/admin/v1/auth/setup` on first run. There is no default password. |
| `YAGO_ADMIN_CORS_ORIGINS` | _(empty)_ | Comma-separated origin allowlist for cross-origin browser requests to the operations surface. Empty denies all cross-origin requests. Use `*` to echo any origin (required for a credentialed admin UI on an unknown origin, but broad). |
| `YAGO_SEARCH_CORS_ORIGINS` | _(empty)_ | Comma-separated origin allowlist for cross-origin browser requests to the public search endpoints on the peer listener. Empty denies all cross-origin requests; `*` allows any origin without credentials. |

## Admin authentication

Every operations endpoint except `/health` and `/ready` requires a valid admin
session. Provision the administrator with `YAGO_ADMIN_USER` and
`YAGO_ADMIN_PASSWORD`, or, when those are unset, create the first administrator on
first run with `POST /api/admin/v1/auth/setup` (allowed only while no admin
exists). `POST /api/admin/v1/auth/login` verifies the Argon2id-hashed password and
returns an `HttpOnly`, `SameSite=Strict` session cookie plus a CSRF token; send the
cookie on later requests and the `X-CSRF-Token` header on unsafe methods (`POST`,
`PUT`, `PATCH`, `DELETE`). `POST /api/admin/v1/auth/logout` invalidates the
server-side session, `GET /api/admin/v1/auth/session` returns the current
administrator, login is rate limited per client, and a failed login does not reveal
whether the account exists. The session cookie is marked `Secure` only when the
request arrives over TLS, so terminate TLS at the node or a trusted reverse proxy.
Login and API-key outcomes are exported on `/metrics` as
`admin_login_attempts_total` (results `success`, `failure`, `throttled`) and
`admin_api_key_auth_total` (results `rejected`, `throttled`, `forbidden`) so
operators can alert on brute-force pressure.

For non-interactive clients, create an API key with
`POST /api/admin/v1/auth/api-keys` (a session or an `admin:write` key is required
to manage keys). The response returns the secret exactly once; the node stores
only its SHA-256 hash alongside a public identifier. Send the key as
`Authorization: Bearer <key>` instead of a cookie; it is checked against the scope
the path requires, is rate limited per key, and needs no CSRF token. Scopes are
`admin:read` (read-only operations such as `/metrics`), `admin:write` (state
changes and key management), `crawl:write` (`POST /crawl`), and the reserved
`search:read` and `search:raw` for the public search API. `GET
/api/admin/v1/auth/api-keys` lists key metadata and last-used time without the
secret, and `DELETE /api/admin/v1/auth/api-keys/{id}` revokes a key. Prometheus
can scrape `/metrics` with an `admin:read` key or a logged-in session cookie;
otherwise bind `YACY_OPS_ADDR` to a trusted network. `GET
/api/admin/v1/events` returns a bounded, in-memory log of recent structured
events (severity and category, newest first, optional `limit`) under the same
`admin:read` scope, including a node-started event and admin login and API key
auth outcomes; the log is not persisted across restarts.

## Cross-origin requests and network binding

Cross-origin requests are denied by default. Browser code on another origin can
call the operations surface only when its origin is listed in
`YAGO_ADMIN_CORS_ORIGINS`, and the public search endpoints only when its origin is
listed in `YAGO_SEARCH_CORS_ORIGINS`. A request without an `Origin` header — every
peer-to-peer `/yacy/*` call and any same-origin call — is never affected, so
enabling search CORS does not change peer behavior. The admin policy is
credentialed and echoes an allowlisted origin (a literal `*` cannot be combined
with credentials), so cookies flow only to origins you name; the search policy is
uncredentialed and may use `*`.

The operations surface and the peer protocol listen on separate addresses, so the
admin surface can be kept off the public network. Bind `YACY_OPS_ADDR` to loopback
(for example `127.0.0.1:9090`) or a private interface and reach it through a
trusted reverse proxy, while `YACY_PEER_ADDR` stays on the public interface for
P2P. Terminate TLS at that proxy so the session cookie is marked `Secure`.

## Crawling

The node can drive a crawl fleet over gRPC: it serves a `CrawlExchange` endpoint that crawlers dial. Operators start a crawl by posting seed URLs to `/crawl` on the ops address; the node enqueues orders in a durable, store-backed FIFO and streams them to connected crawlers, and crawled pages flow back in as ingest batches. Crawling is off until `YACY_CRAWL_RPC_ADDR` is set; without it the node behaves as a pure peer.

The `/crawl` request body accepts `seeds` and optional `startMode`. Supported
start modes are `url`, `sitemap`, `sitelist`, and `robots`; empty mode is treated
as `url`. Sitemap and sitelist starts are expanded by the crawler into bounded URL
roots before normal frontier admission. A `robots` start reads each seed host's
`robots.txt`, expands the sitemaps named in its `Sitemap:` directives, and admits
those URLs; a missing or unreadable `robots.txt` discovers nothing rather than
failing the crawl.

| Variable | Default | Description |
| --- | --- | --- |
| `YACY_CRAWL_RPC_ADDR` | _(empty)_ | Address the node serves the crawl gRPC endpoint on (e.g. `:9091`). Empty disables crawling. |
