# Configuration

The node is configured through environment variables.

> **Environment naming:** the node variables below were renamed from the `YACY_`
> prefix to `YAGO_`. For one release, an unset `YAGO_` variable falls back to its
> legacy `YACY_` name and logs a one-time deprecation warning. Crawler variables
> use `YAGO_CRAWLER_` exclusively. The v0.0.12 package migration rewrites
> line-leading `YAGOCRAWLER_` keys in the legacy package-managed environment file;
> the runtime provides no fallback for that spelling or the earlier
> `YACYCRAWLER_` spelling.

## Runtime overrides

A small, whitelisted set of settings can also be changed at runtime from the
admin console's Configuration section. An override is stored durably in the node
vault and takes precedence over the environment default; clearing it (**Reset to
default**) reverts to the environment value. Overrides survive restarts, require
an authenticated admin session with a CSRF token, and record a `config` event on
each change. The controlled-network shared secret is the only sensitive
override: it is accepted only as a write-only value and is never returned in a
view or event. The public search portal
toggle and the HTTP→HTTPS redirect apply live (no restart); listen-address
changes take effect on the next restart, which the console flags.

The **public search portal** toggle overrides `YAGO_PUBLIC_SEARCH_UI_ENABLED`:
enabling it mounts the portal at the site root, disabling it serves the static
landing page there instead, switched live on the next request. While the portal
is enabled it advertises itself as a browser search engine: the portal page
carries an OpenSearch autodiscovery link and the node serves an OpenSearch
description document at `/opensearch.xml` (pointing at the portal root's `q`
parameter) plus a suggestions endpoint at `/opensearch/suggest`. Both are served
only while the portal is on, expose only public search, and — honouring the
SEC-05 privacy stance — the suggestions endpoint keeps no query history and
always returns an empty completion list. The page advertisement and description
share one OpenSearch engine name within the 16-character browser limit; stored
copies of the earlier default theme are repaired when loaded, so Firefox can
offer the portal as a search engine after an upgrade. This is separate from the
YaCy-compatible `/opensearchdescription.xml`, which describes the `/yacysearch.*`
endpoints. The **HTTP→HTTPS
redirect** toggle overrides `YAGO_HTTPS_REDIRECT` (off by default): when on, a
plain-HTTP request is answered with a 308 to the `https://` origin, preserving
the path and query. TLS is expected to be terminated in front (a reverse proxy
sets `X-Forwarded-Proto`); loopback requests are never redirected, so the admin
console reached over `localhost` cannot be pushed to an unreachable HTTPS origin.

### Listen addresses

The node runs four separate listeners, each with a distinct job:

- **Peer protocol** (`YAGO_PEER_ADDR`, default `:8090`) — the YaCy peer-to-peer
  wire protocol only: `/yacy/*` (search fan-out, RWI/URL transfer, seedlist,
  query, hello). Other peers reach you here, so keep it reachable from the
  network. Its root `/` serves a static identity landing page.
- **Public search** (`YAGO_PUBLIC_ADDR`, default `:8080`) — the client-facing
  surfaces: the Tavily-compatible API (`POST /search`, `POST /extract`), the
  `/yacysearch.*` endpoints, OpenSearch description/suggestions, and the public
  search portal at its root `/`. Set `YAGO_PUBLIC_ADDR` to `off` (or `none`) to
  run a pure peer node with no public surface. Front it with a reverse proxy for
  TLS; the default `:8080` is unprivileged so the `nonroot` container binds it
  without extra capabilities.
- **Admin and ops** (`YAGO_OPS_ADDR`, default `:9090`) — `/health`, `/ready`,
  `/metrics`, the ops JSON endpoints, and the admin console at `/admin/`. Its
  root `/` redirects to the console. Every path except `/health` and `/ready`
  requires an admin session or a scoped API key, so this listener can be bound to
  loopback (`127.0.0.1`) when the console is only reached locally or through a
  proxy.
- **Crawler exchange** (`YAGO_CRAWL_RPC_ADDR`, default `127.0.0.1:9091`) — the
  gRPC control, order, progress, and ingest channel used by crawler processes.
  `off` starts no crawler exchange listener. Bind it beyond loopback only when a
  remote crawler must connect and the surrounding network is trusted.

The Configuration section also has a per-surface bind editor. It lists the host's
network interface addresses (including loopback) and lets you set, per listener,
the interface (or **all interfaces**) and the port for each of the four surfaces
above. A bind override is validated — the host must be one of the machine's own
interface addresses, so you cannot bind a surface to an unreachable address —
persisted durably, and applied on the next restart. The public and crawler
listeners can also be disabled there. **Reset** deletes any of the four stored bind overrides and
restores the corresponding environment bootstrap value. Loopback and
all-interfaces are always offered so a bind change cannot lock you out of the
admin console.
Changing the **peer** listener's port also re-derives the advertised port and the
local DHT self-test address from the new value, so the node announces the port it
actually listens on — unless you pinned them explicitly with `YAGO_ADVERTISE_PORT`
or `YAGO_PUBLIC_SELF_TEST_URL`, which remain pinned instead of being re-derived (set the latter when the
peer listener is behind a reverse proxy or NAT and the external address differs).
Successful outbound peer greetings also carry the observer's external back-ping
classification. A YaCy peer may return this node's advertised DNS name in
`yourip` after a successful callback; the client accepts that name when it
matches the local seed instead of requiring an IP literal. Any current peer
observation is authoritative: `senior` or
`principal` confirms that the advertised endpoint is reachable, while an
all-`junior` observation set reports it unreachable. Only when no current peer
observation exists does the node consult the direct self-test, and it executes
that request only when `YAGO_PUBLIC_SELF_TEST_URL` was explicitly configured.
The automatically derived loopback or listener address is not treated as public
reachability evidence. This prevents a router without NAT hairpin support from
overriding successful external ingress and prevents a local loopback response
from proving public ingress. Reachability is an inbound status observation and
does not close outbound DHT distribution.

The node binds every configured peer, operations, and public-search HTTP
listener before starting its peer-presence loops. If any HTTP bind fails, it
closes the listeners already opened and starts no announcement work. A first
outbound greeting therefore cannot classify the node before its advertised peer
HTTP endpoint is listening.

### Storage admission

`YAGO_STORAGE_QUOTA` is the soft admission and eviction target for logical live
rows in the main sharded vault. It excludes Bleve, `crawlbroker.db`, crawler
frontier databases, bbolt free pages, open-but-deleted blocks, and temporary
copies. It is not a filesystem quota.

The node and crawler use independent filesystem free-space policies.
`YAGO_STORAGE_RESERVED_FREE` and `YAGO_STORAGE_PRESSURE_HYSTERESIS` apply to
gate-managed node growth. Their Admin keys are `storage.reserved_free` and
`storage.pressure_hysteresis`. `YAGO_CRAWLER_STORAGE_RESERVED_FREE` and
`YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS` bootstrap each crawler's policy; the
matching Admin keys are `crawler.storage_reserved_free` and
`crawler.storage_pressure_hysteresis`. Before the crawler opens its checkpoint,
the node returns both current Admin values in the startup runtime-policy
envelope, including explicit zero. Omission by an older node preserves the
environment bootstrap. The node sends later live crawler-policy changes on
worker heartbeats.

Two independent soft physical state-file boundaries complement filesystem
pressure. `YAGO_CRAWLER_NODE_STATE_MAX_BYTES` and Admin key
`crawler.node_state_max_bytes` control `crawlbroker.db` on the node.
`YAGO_CRAWLER_FRONTIER_STATE_MAX_BYTES` and Admin key
`crawler.frontier_state_max_bytes` control `crawler/frontier-v1.db`; keep its
bootstrap equal in both services, after which the node distributes the
authoritative value through typed crawler policy. Both default to 4 GiB, apply
live, and accept `0` to disable the boundary.

At or above the node boundary, fresh order enqueue is rejected while migration,
ingest, lifecycle, recovery, and settlement writes continue. At or above the
crawler boundary, fresh orders wait before expansion and discovered-link batches
are refused. An order whose committed seed manifest crosses the boundary remains
existing durable work and completes; queued dispatch, recovery, lifecycle, and
terminal settlement also continue. Raising or disabling the crawler boundary
wakes waiting orders.

On startup, either state file is compacted when its physical size is equal to or
greater than its enabled boundary. Each process holds a persistent path-stable
sidecar lease through stale-copy cleanup, compaction, atomic replacement,
directory sync, inspection, and authoritative database open. Inside the shared
serialized storage-maintenance gate, it remeasures the actual source size,
reserves that temporary headroom, and copies live bbolt rows into a synced
private sibling file. Still under the sidecar lease, it atomically replaces the
original and syncs the directory. A failure before replacement is a recoverable
warning and startup opens the unchanged original; a later directory-sync or
inspection failure is reported as an installed durability warning. The
boundaries are admission controls, not filesystem quotas.

Each process measures the filesystem containing its own `YAGO_DATA_DIR`.
Measurement failure pauses admission. At or below the reserve, gate-managed node
crawl/index ingestion pauses and crawler frontier growth plus fetch admission
waits. Admission resumes only after free space reaches the reserve plus
hysteresis. Deletion, eviction, settlement, and existing crawl-state recovery
remain available where startup has otherwise completed. Large node compaction,
shard-split, and legacy migration copies reserve operation-specific headroom
after a fresh serialized measurement.

These checks are advisory: ordinary writes can allocate after their preflight,
and bounded bootstrap writes and storage-engine background work are not an
aggregate transaction. Deleting bbolt rows may create reusable database pages
without increasing operating-system free space. Free filesystem space or lower
the applicable reserve and hysteresis when pressure persists. Use a filesystem
or project quota, or a quota-capable volume, when an exact aggregate maximum is
required.

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | Bootstrap log threshold: `DEBUG`, `INFO`, `WARN`, or `ERROR`. Admin key: `logging.level` in Configuration → Monitoring; persisted changes and Reset apply live to the active node logger. |
| `YAGO_PEER_BIRTH_DATE` | first start time | Optional peer birth date (`YYYYMMDD`, UTC) stored on the first start with a fresh data directory. Declare it when migrating an established peer identity or when private-network tests need an aged peer; YaCy peers skip DHT targets younger than three days. |
| `YAGO_DATA_DIR` | `./data` | Where each process persists its data. The node uses `yago-node.db` (or an existing `yacy-rwi.db`), `search.bleve`, peer profile, shared-blacklist files, and `crawlbroker.db` for atomic crawler coordination. The crawler uses `crawler/frontier-v1.db` for exact restart recovery and its stable worker identity. Package services share `/opt/yago/data` under one OS user; the Docker images use separate volumes because their unprivileged UIDs differ. |
| `YAGO_PEER_ADDR` | `:8090` | Listen address for the YaCy peer protocol. Dedicated Admin key: `bind.peer`; a stored address takes effect after restart, and Reset deletes the override so this environment value is authoritative again. |
| `YAGO_PUBLIC_ADDR` | `:8080` | Listen address for public search, agent APIs, and OpenSearch. `off`, `none`, or `disabled` starts no public listener. Dedicated Admin key: `bind.public`; Disable persists the off state, and Reset deletes the override so this environment value is authoritative again. Changes take effect after restart. |
| `YAGO_OPS_ADDR` | `:9090` | Listen address for `/health`, `/ready`, `/metrics`, ops JSON endpoints, and the Admin console. Dedicated Admin key: `bind.ops`; a stored address takes effect after restart, and Reset deletes the override so this environment value is authoritative again. |
| `YAGO_METRICS_ENABLED` | `true` | Serve the Prometheus `/metrics` endpoint. Set to `false` to unmount it (returns 404); the collectors still run. The endpoint is admin-authenticated regardless (it is not on the operations listener's public allowlist). See [metrics.md](metrics.md). |
| `YAGO_ADMIN_RESTART_ENABLED` | `true` | Offer the node/crawler restart controls in the admin console. Admin key: `admin.restart_controls.enabled`; a stored change takes effect after the next node restart. The setup wizard's mandatory post-setup restart is unaffected. |
| `YAGO_PEER_HASH` | _(generated)_ | The 12-character enhanced-Base64 peer hash advertised to the network. If unset, the node generates one on first start and persists it to the data directory, reusing it across restarts so the peer keeps a stable identity. Set it to pin a specific hash. |
| `YAGO_PEER_NAME` | _(generated)_ | Peer name advertised to the network. Generated (as `yago-<random>`) and persisted like the hash when unset. |
| `YAGO_NETWORK_NAME` | `freeworld` | Exact YaCy network unit to join (one visible line, at most 128 bytes). Admin key: `network.name`; a stored change takes effect after restart and isolates the node from peers on the previous network. |
| `YAGO_NETWORK_AUTHENTICATION` | `uncontrolled` | YaCy peer-protocol authentication mode. `uncontrolled` interoperates with the public network; `salted-magic-sim` requires every peer on a controlled private network to use the same shared secret. Admin key: `network.authentication.mode`; takes effect after restart. |
| `YAGO_NETWORK_AUTHENTICATION_SECRET` | _(empty)_ | Shared secret for `salted-magic-sim`. The Admin console stores an override locally, renders it only as configured/not configured, and never returns it in HTML or configuration events. Admin key: `network.authentication.secret`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_ENABLED` | `false` | Enables YaCy remote-crawl delegation only when salted-magic authentication, a nonempty shared secret, trusted peer hashes, and allowed destinations are also configured. Admin key: `swarm.remote_crawl.enabled`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_TRUSTED_PEERS` | _(empty)_ | Comma-separated exact 12-character YaCy peer hashes allowed to lease work and submit receipts (1–256 while enabled). Admin key: `swarm.remote_crawl.trusted_peers`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_ALLOWED_DESTINATIONS` | _(empty)_ | Comma-separated exact domains and IP prefixes eligible for delegation (1–256 while enabled); address-family wildcard prefixes are rejected. Only HTTP and HTTPS on their default ports are accepted; every DNS answer is revalidated at staging, leasing, and receipt time. Admin key: `swarm.remote_crawl.allowed_destinations`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_REQUESTS_PER_MINUTE` | `60` | Maximum authenticated remote-crawl feed requests per trusted peer in one fixed minute window (1–10,000). Admin key: `swarm.remote_crawl.requests_per_minute`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_OUTSTANDING_PER_PEER` | `10` | Maximum simultaneously leased remote-crawl URLs for one trusted peer (1–100). Admin key: `swarm.remote_crawl.outstanding_per_peer`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_LEASE_TTL` | `10m` | Durable lease lifetime before unfinished work returns to the delegation queue (1 second–24 hours). Admin key: `swarm.remote_crawl.lease_ttl`; takes effect after restart. |
| `YAGO_REMOTE_CRAWL_QUEUE_CAPACITY` | `1000` | Maximum distinct locally accepted URLs retained in the durable delegation queue (1–100,000). A full delegation queue never rejects or removes the authoritative local crawler order. Admin key: `swarm.remote_crawl.queue_capacity`; takes effect after restart. |
| `YAGO_SEEDLIST_URLS` | _(empty)_ | Comma-separated YaCy seedlist URLs to discover peers from. |
| `YAGO_LAN_DISCOVERY` | `false` | Announce this node over the local UDP discovery beacon and greet announcing neighbors through the verified YaCy hello exchange. Admin key: `network.lan_discovery`; takes effect after restart. |
| `YAGO_ADVERTISE_HOST` | _(auto)_ | Public IP or DNS name other peers use to reach you. When unset and the node announces to the network (`YAGO_SEEDLIST_URLS` set), it auto-detects the first non-loopback IPv4 address. Set it explicitly behind NAT or Docker bridge networking, where the guess is wrong. Validated external hello observations classify public reachability; without current peer evidence or an explicitly pinned public self-test URL, the status remains unconfirmed without disabling outbound DHT distribution. |
| `YAGO_ADVERTISE_PORT` | _(the `YAGO_PEER_ADDR` port)_ | Port other peers use to reach you. Admin key: `network.advertise.port`; an empty value follows the peer listener, and a pinned 1–65535 value takes effect after restart. |
| `YAGO_PEER_ADVERTISE_DIRECT` | `true` | Advertise the YaCy direct-connect capability. Admin key: `peer.advertise.direct_connect`; takes effect after restart when the seed identity is rebuilt. |
| `YAGO_PEER_ADVERTISE_REMOTE_INDEX` | `true` | Advertise and accept inbound YaCy RWI transfers. Admin key: `peer.advertise.remote_index`; when off, inbound transferRWI and transferURL calls are refused, and the change takes effect after restart. |
| `YAGO_PEER_ADVERTISE_ROOT_NODE` | `false` | Advertise the YaCy root-node capability. Admin key: `peer.advertise.root_node`; takes effect after restart. |
| `YAGO_PEER_ADVERTISE_SSL` | `false` | Advertise that the peer port terminates HTTPS. Admin key: `peer.advertise.ssl`; enable it only when the advertised port actually serves TLS. The change takes effect after restart. |
| `YAGO_PUBLIC_SELF_TEST_URL` | _(empty)_ | Explicit public base URL eligible for the bounded reachability fallback query to `/yacy/query.html?object=rwicount` when no current peer observation exists. Admin key: `network.public_self_test_url`; set an absolute public HTTP(S) URL behind a reverse proxy or NAT. Empty leaves public reachability dependent on peer back-ping evidence and otherwise unconfirmed; the derived local peer URL is not queried as public evidence. This status does not gate outbound DHT distribution. Bootstrap and Admin share one 2,048-byte canonicalizer; credentials, query strings, fragments, opaque URLs, control characters, and invalid hosts or ports are rejected. |
| `YAGO_ANNOUNCE_INTERVAL` | `10m` | How often to re-announce yourself to the network (e.g. `30s`, `10m`, `1h`). |
| `YAGO_GREETS_PER_CYCLE` | `16` | How many peers to greet in each announce cycle. |
| `YAGO_NETWORK_DHT` | `true` | Enables the sender-side DHT gate equivalent to YaCy `network.unit.dht`. |
| `YAGO_DHT_DISTRIBUTION` | `true` | Enables the sender-side DHT distribution gate equivalent to YaCy `allowDistributeIndex`. |
| `YAGO_DHT_ALLOW_WHILE_CRAWLING` | `false` | Allows outbound DHT distribution while local crawling is active. |
| `YAGO_DHT_ALLOW_WHILE_INDEXING` | `true` | Allows outbound DHT distribution while local indexing is active. |
| `YAGO_DHT_DISTRIBUTION_INTERVAL` | `10s` | How often the outbound DHT scheduler runs a distribution cycle. |
| `YAGO_DHT_REDUNDANCY` | `3` | Number of DHT target peers per vertical partition for outbound transfer and global remote search. Accepted values are `1` through `16`; the default matches YaCy freeworld senior peers. |
| `YAGO_DHT_PARTITION_EXPONENT` | `4` | YaCy vertical DHT partition exponent. Admin key: `dht.partition_exponent`; accepted values are `0` through `8` and take effect after restart. Keep 4 for YaCy freeworld: peers using different geometry route incompatible vertical partitions. |
| `YAGO_DHT_MINIMUM_PEER_AGE_DAYS` | `3` | Minimum peer age for DHT target eligibility. Set `-1` only for controlled tests or private networks that intentionally disable the age gate. |
| `YAGO_DHT_MINIMUM_CONNECTED_PEERS` | `33` | Sender-side DHT gate: minimum connected peers before outbound distribution starts. Lower it only for private networks or controlled tests. |
| `YAGO_DHT_MINIMUM_RWI_WORDS` | `100` | Sender-side DHT gate: minimum locally stored RWI words before outbound distribution starts. |
| `YAGO_TRUSTED_PROXIES` | _(empty)_ | Comma-separated CIDRs or IPs of reverse proxies fronting the node. Set this when running behind a reverse proxy so peers are not told the proxy's address. |
| `YAGO_STORAGE_QUOTA` | `1GB` | Soft admission and eviction target for logical live main-vault rows, as a human-readable size. It excludes Bleve, crawl state, allocated free pages, and temporary copies. Admin key: `storage.quota`. |
| `YAGO_STORAGE_RESERVED_FREE` | `1GB` | Filesystem free-space reserve for gate-managed node growth. Admin key: `storage.reserved_free`; applies live. |
| `YAGO_STORAGE_PRESSURE_HYSTERESIS` | `256MB` | Additional free space above the node reserve required before gate-managed growth resumes. Admin key: `storage.pressure_hysteresis`; applies live. |
| `YAGO_CRAWLER_NODE_STATE_MAX_BYTES` | `4GB` | Soft physical admission boundary for `crawlbroker.db`; `0` disables it. Admin key: `crawler.node_state_max_bytes`; applies live. |
| `YAGO_STORAGE_COMPACTION_INTERVAL` | `1d` | Cadence for rewriting storage shards so deleted space returns to the operating system; `off` disables compaction. Admin key: `storage.compaction.interval`; applies live. |
| `YAGO_STORAGE_AUTOSPLIT` | `true` | Allow the storage engine to grow its linear-hashing shard pool as data accumulates. Admin key: `storage.autosplit`; applies live. Turning it off freezes the current shard geometry. |
| `YAGO_STORAGE_DEFER_FSYNC` | `false` | Skip the per-commit storage flush and use bounded periodic flushing. Admin key: `storage.defer_fsync`; takes effect after restart. Leave it off unless the host has reliable power and a bounded crash-loss window is acceptable. |
| `YAGO_STORAGE_READ_DEFER` | engine default (50ms) | Maximum time each storage write yields to active interactive reads. Admin key: `storage.read_defer`; `0s` selects the 50ms engine default, a negative duration disables yielding, and a stored change takes effect after restart. |
| `YAGO_CRAWLER_STORAGE_RESERVED_FREE` | `1GB` | Filesystem free-space reserve bootstrapped by each crawler and sent live by the node. Admin key: `crawler.storage_reserved_free`. |
| `YAGO_CRAWLER_STORAGE_PRESSURE_HYSTERESIS` | `256MB` | Additional free space above the crawler reserve required before frontier growth and fetch admission resume. Admin key: `crawler.storage_pressure_hysteresis`. |
| `YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS` | `false` | Allow outbound connections to RFC 1918 and unique-local addresses. Enable only for LAN or private-network deployments; loopback, link-local, and reserved ranges stay blocked. |
| `YAGO_EGRESS_ALLOW_CIDRS` | _(empty)_ | Comma-separated private CIDRs the egress guard admits even when `YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS` is false, so intranet mode reaches only named ranges instead of all private space. Only relaxes the private check: loopback, link-local (including the cloud metadata range), and reserved ranges stay blocked, so a non-private entry never grants access to them. |
| `YAGO_SEARCH_API_KEY` | _(empty)_ | Optional legacy static bearer token for the Tavily-compatible `POST /search`, `POST /extract`, `POST /crawl`, and `POST /map` endpoints. Callers send `Authorization: Bearer <token>`. This local node credential is not a key for an external search service; the node uses no keyed external search API. It is accepted only while `YAGO_SEARCH_REQUIRE_API_KEY` is off; Admin-minted keys holding the required scope authenticate in either mode. When neither a usable static token nor a valid scoped key is presented, the agent endpoints deny access. |
| `YAGO_SEARCH_REQUIRE_API_KEY` | `false` | Enforce scoped-only credentials on the Tavily-compatible surface. Admin-minted keys holding the required scope authenticate whether this switch is on or off: ordinary `/search` needs `search:read`, while raw-content search, extract, crawl, and map need `search:raw`. When this switch is off, the configured legacy `YAGO_SEARCH_API_KEY` is also accepted; when on, that static token is rejected. Missing/invalid keys return `401`, insufficient scope `403`, rate-limited keys `429`, and credential-store failures `503`. |
| `YAGO_PUBLIC_SEARCH_UI_ENABLED` | `false` | Serve the anonymous public search portal on the public listener's root (`/`). Off by default; while off, the root serves the landing page and the portal is not mounted. When on, a minimal, server-rendered, no-JavaScript search page runs exact/morphological retrieval against the local index plus YaCy peers. An empty incomplete exact stage gets bounded local-exact rescue; an honest miss gets bounded local fuzzy recovery. The `enabled` DDGS mode runs only after the applicable local recovery also misses; `always` runs DDGS alongside local and swarm retrieval. It exposes only search — never admin APIs — and logs query text only when the operator explicitly selects `YAGO_QUERY_LOG_MODE=full`; the default and aggregate modes omit it. Overridable live from the admin console (see Runtime overrides). |
| `YAGO_PUBLIC_BASE_URL` | _(derive from request)_ | Absolute HTTP(S) public base used by OpenSearch descriptors and public links behind a reverse proxy. An empty value derives the base from each request. Admin key: `public.base.url`; applies live. |
| `YAGO_HTTPS_REDIRECT` | `false` | Redirect plain-HTTP requests to the `https://` origin with a 308, preserving path and query. Off by default. TLS termination is expected in front (a reverse proxy sets `X-Forwarded-Proto`); loopback requests are never redirected. Overridable live from the admin console (see Runtime overrides). |
| `YAGO_EXTRACT_FETCH_ENABLED` | `false` | Enable fetch-on-extract for `POST /extract`. Each stored-document lookup is capped at 250 milliseconds. Off by default, so an uncached URL or a lookup timeout is a controlled per-URL `failed_results` entry with no outbound request. When on, an uncached URL or timed-out lookup uses the request's remaining budget to fetch through the shared egress-guarded client (private networks stay default-denied — no SSRF) and extract its title and visible text. A request deadline or fetch failure is also reported for that URL, preserving completed rows in a mixed HTTP 200 response. |
| `YAGO_EXTRACT_FETCH_TIMEOUT` | `10s` | Per-request timeout for a fetch-on-extract fetch. |
| `YAGO_EXTRACT_FETCH_MAX_BYTES` | `2097152` | Maximum response bytes read per fetch-on-extract fetch. The default is 2 MiB; configuration accepts 1 byte through the 4 MiB hard ceiling and rejects a larger value. A response above the configured limit is rejected, not truncated into a partial document. |
| `YAGO_ADMIN_USER` | _(empty)_ | Administrator username. When set with `YAGO_ADMIN_PASSWORD`, the admin is provisioned on every start and those credentials are authoritative. |
| `YAGO_ADMIN_PASSWORD` | _(empty)_ | Administrator password, stored as an Argon2id hash. Leave both admin variables empty to create the first admin with `POST /api/admin/v1/auth/setup` on first run. There is no default password. |
| `YAGO_ADMIN_CORS_ORIGINS` | _(empty)_ | Comma-separated origin allowlist for cross-origin browser requests to the operations surface. Empty denies all cross-origin requests. Use `*` to echo any origin (required for a credentialed admin UI on an unknown origin, but broad). |
| `YAGO_SEARCH_CORS_ORIGINS` | _(empty)_ | Comma-separated origin allowlist for cross-origin browser requests to the public search endpoints on the public listener. Empty denies all cross-origin requests; `*` allows any origin without credentials. |
| `YAGO_SEARCH_REMOTE_PEER_TIMEOUT` | `1200ms` | Maximum contribution time for one YaCy peer inside an interactive search. |
| `YAGO_SEARCH_REMOTE_TIMEOUT` | `1300ms` | Aggregate YaCy peer fan-out budget. The public pipeline enforces a 1.8-second end-to-end deadline. Public search endpoints admit no more than 16 concurrent requests; an admitted request waits for one of four outer pipeline slots only within that deadline instead of reporting a false miss during a short burst. An empty incomplete exact stage gets one local-exact retry inside the same boundary: 150 milliseconds for ordinary incomplete failures and 500 milliseconds only when the exact stage reports capacity exhaustion. The capacity-only retry may wait for its own four-slot admission within those 500 milliseconds; ordinary and fuzzy retry admission remains nonblocking. Miss-triggered web fallback caps its complete exact local-plus-swarm and peer-evidence stage at 600ms so local recovery and the sequential provider retain time. `always` mode runs the provider concurrently and gives that primary stage up to 1400ms, so a cold local index can still publish its rows without extending the hard response boundary. Four remote branches may run process-wide; a branch retains that admission until peer fan-out actually returns even when its local answer has already been released. One query additionally shares fixed limits of 8 MiB response data, 1,024 metadata rows, and 8,192 abstract hashes across exact and morphology passes; each query starts at most 32 peer HTTP attempts. The default primary fan-out retains at most 16 peers, enough for one eligible peer in every freeworld exponent-4 vertical partition and enough remaining calls for one secondary retrieval per primary peer. Ordinary attempts share 32 process-wide slots; exact multiword primary calls request `abstracts=auto` and do not issue separate exact-term probes. Optional morphology abstract jobs can consume at most 20 calls per query and additionally share eight process-wide morphology slots; single-word variants and secondary metadata calls use the total and ordinary process ceilings. Enabled multiword swarm morphology retains one exact primary request and adds an index-abstract plan with at most 12 corpus-observed or analyzer-verified generated forms per original requirement, 20 forms across the query, and two peers per form. It unions forms within a requirement and intersects across requirements, so work is linear and capped rather than Cartesian. These limits preserve partial swarm results and do not penalize a peer when local admission is saturated. |
| `YAGO_PEER_SNIPPET_FETCH` | `true` | Permit bounded, egress-guarded body fetches for the first peer rows whose visible title, snippet, and decoded URL do not prove every query requirement. Admin setting: `search.peer.snippet_fetch`. |
| `YAGO_SWARM_MORPHOLOGY` | `false` | Add bounded corpus-observed and analyzer-verified surface forms to YaCy swarm retrieval while preserving exact RWI wire hashes. Admin setting: `swarm.morphology.enabled`. |
| `YAGO_WEB_FALLBACK_ENABLED` | _(migration only)_ | Legacy on/off input accepted only when `YAGO_WEB_FALLBACK_PRIVACY` is unset (`true` becomes `enabled`, `false` becomes `disabled`). Canonical deployment examples omit it. |
| `YAGO_WEB_FALLBACK_PRIVACY` | `disabled` | Controls the `Web search fallback (DDGS)` Admin setting. `disabled` never sends a query; `explicit` requires request consent; `enabled` runs web search after exact local-plus-swarm and the applicable bounded local recovery miss; `always` starts bounded web retrieval alongside local and swarm for every eligible global query, then rank-fuses and deduplicates all completed results. Tavily `advanced` search grants request consent and follows this global policy; `basic`, `fast`, and `ultra-fast` remain local-only and never use the provider. YaCy `resource=local` and admin `scope=local` also never use the provider. |
| `YAGO_WEB_FALLBACK_PROVIDER` | _(migration only)_ | Legacy provider-family input; if present it must be exactly `ddgs`. The runtime provider is fixed, while `YAGO_WEB_FALLBACK_BACKEND` selects its engine. Canonical deployment examples omit this input. |
| `YAGO_WEB_FALLBACK_BACKEND` | `auto` | Engine selection for the fallback. `auto` starts DuckDuckGo HTML first, then hedges DuckDuckGo Lite, Brave, Mojeek, and Bing at 50ms intervals until one answer survives relevance checks. Internal dash punctuation is sent as word boundaries so engines do not reinterpret a compound query as exclusion; an explicit leading minus and structured modifier values remain intact. At most eight engine fetch-and-parse attempts run process-wide. `mojeek`, `bing`, `brave`, or `duckduckgo` restrict the engine set. See `doc/adr/0021-in-house-metasearch-backend.md`. |
| `YAGO_WEB_FALLBACK_MAX_RESULTS` | `10` | Maximum fallback results (1–20). |
| `YAGO_WEB_FALLBACK_TIMEOUT` | `10s` | Per-engine timeout ceiling. Interactive search additionally caps the complete hedged web stage at 900ms after a local-plus-swarm miss or 1500ms when `always` starts it in parallel, inside the fixed 1.8-second deadline. |
| `YAGO_WEB_FALLBACK_SAFESEARCH` | `moderate` | Safe-search preference passed to engines that support it (`strict`, `moderate`, `off`). |
| `YAGO_WEB_FALLBACK_CACHE_TTL` | `5m` | How long to cache a fallback response to respect engine rate limits and reduce repeat egress. Normalized responses share a fixed 4 MiB/256-entry byte-aware cache, retain at most 20 rows per query, and bound each title, URL, and snippet before insertion. |
| `YAGO_WEB_FALLBACK_SEED_CRAWL` | `false` | When on (and crawling is enabled), URLs surfaced by the fallback are published as conservative crawl orders so later queries can be answered locally. Publishing runs after the search response through two background workers, a process-wide queue of at most 128 pending jobs, and a ten-second deadline that begins when each job starts. A full queue warns and skips only new optional warming work. Each URL gets a 50-millisecond stored-document presence check before an absent or indeterminate URL attempts URL-idempotent durable publication; at most one accepted order remains for that normalized URL. No effect when crawling is disabled. |
| `YAGO_WEB_FALLBACK_SEED_DEPTH` | `5` | Crawl depth for web-discovery orders when web-discovery crawling is enabled (0–8). |
| `YAGO_WEB_FALLBACK_SEED_MAX_PAGES` | `250` | Whole-run page cap for each web-discovery crawl task when enabled. The global crawler run cap may reduce it further. |
| `YAGO_QUERY_LOG_MODE` | `off` | How much of a search query is written to the node's logs. `off` records nothing; `aggregate` records the query length and result count but never the text; `full` records the query text. In either enabled mode, an incomplete response also records `partialFailures` and at most eight ordered unique `failureSources`. The default keeps queries out of the logs. |
| `YAGO_INDEX_REMOTE_RESULTS` | `true` | Cache the metadata of results returned by peers into the local index after a swarm search, mirroring YaCy's `addResultsToLocalIndex`, so a later query for the same content is answered locally without re-fetching. Metadata only (title, snippet, URL — no crawl); a URL already in the store is never overwritten, so a locally crawled full page is preserved. Writes run off the request path, are limited to two concurrent operations with a 30-second deadline, and are skipped while that bounded admission is saturated. Set to `false` to leave the index unchanged by searches (a node that indexes only what it crawls). |
| `YAGO_PEER_HTTPS_PREFERRED` | `false` | Prefer HTTPS for outbound YaCy peer-protocol calls (hello, remote search, transferRWI/transferURL, back-ping) to peers that advertise an SSL port and the SSL seed flag, mirroring YaCy's `network.unit.protocol.https.preferred`. A failed HTTPS transport attempt retries the same peer over plain HTTP, YaCy-style. Peer certificates in the wild are self-signed, so certificate verification is disabled for the peer-protocol client only — peer authenticity on the YaCy wire comes from protocol-level checks (target hash, network name, hello magic), not PKI — and the egress guard still applies. |
| `YAGO_SEARCH_LINKS_NEW_TAB` | `false` | Open result links on the public portal, admin console search, and /yacysearch.html in a new tab. Off by default per NN/G guidance (opening new tabs breaks the back button and takes control from the user); when on, links carry `rel="noopener noreferrer nofollow"` and an accessible "opens in new tab" indicator (visible ↗ plus screen-reader text). |
| `YAGO_SEARCH_CLICK_CAPTURE` | `false` | Record query-to-result click aggregates for offline YagoRank relevance learning. Admin key: `search.click.capture`; takes effect after restart. Query text is not added to ordinary logs by this setting. |
| `YAGO_INGEST_QUALITY_GATE` | `true` | Apply the deterministic web-page quality gate before crawled HTML or plain text is stored and indexed. Admin key: `crawl.ingest.quality_gate`; takes effect after restart. Parsed document formats and unsegmented scripts follow the bounded exceptions described in the feature catalog. |
| `YAGO_SWARM_SEED_CRAWL` | `true` | Greedy learning (YaCy 1.5): enqueue a domain-scoped crawl order (depth 5, up to 250 pages for the complete task, idempotent by URL, skipping URLs already stored) for every URL surfaced by swarm search, so the index grows from what the network already answers with. Requires crawler integration; orders respect robots.txt and blacklist handling. |
| `YAGO_SWARM_SEED_DEPTH` | `5` | Crawl depth for greedy-learning orders (0–8). |
| `YAGO_SWARM_SEED_MAX_PAGES` | `250` | Whole-run page cap for each greedy-learning crawl task. The global crawler run cap may reduce it further. |

## Privacy

By default, no query is sent to an external web-search provider and no query text
is logged. Global search may still send query terms to YaCy peers; peer
federation and external-provider egress are separate controls.

**Query logging (`YAGO_QUERY_LOG_MODE`).** A single decorator wraps the composed
searcher, so every search surface — the human portal, the YaCy-compatible
endpoints, and the Tavily `POST /search` drop-in — is covered by one setting.
`off` (default) records nothing. `aggregate` records the query length and result
count but never the query text; when the response is incomplete, it also records
the total partial-failure count and at most eight ordered unique failure sources.
`full` records the same operational fields plus the query text. Aggregate mode
can therefore diagnose which bounded search stage failed without retaining what
people searched for. Web-provider outage diagnostics never include the provider
request URL.

**External web-search egress (`YAGO_WEB_FALLBACK_PRIVACY`).** The node can consult
an external keyless metasearch provider. The provider necessarily receives the
query, and any pages it returns may be queued for this node to crawl (see
`YAGO_WEB_FALLBACK_SEED_CRAWL`). `disabled` (default) never contacts the provider;
`explicit` contacts it only for a request that opted in; `enabled` waits for
exact local-plus-swarm and the applicable bounded local recovery to miss;
`always` starts the provider alongside local and swarm retrieval and merges all
completed rankings. Internal provenance remains `ddgs`; the public portal and
Admin render plain `web`, YaCy HTML renders `[web]`, and Tavily-compatible
payloads carry no provider marker. Human surfaces state that the provider
received the query.

The former `YAGO_WEB_FALLBACK_TRIGGER` input is accepted only to migrate old
deployments. `enabled` plus legacy `parallel` becomes `always`; saved legacy
timing overrides are collapsed into one versioned, authoritative DDGS mode
record. New Admin updates and resets write only that record, so a legacy trigger
cannot partially change the selected mode. The trigger is no longer a separate
Admin UI setting or a canonical deployment variable.

**Ranking controls.** The YagoRank console persists all 13 operator-safe live
coefficients: five field boosts, host authority, freshness, content quality,
short-URL prior, ordered and unordered proximity, lexical blend, and
original-gap agreement. They apply to the next search without a restart and are
not bootstrap environment variables. Candidate and evidence windows,
evidence-confidence rules, relaxed admission, RM3 drift limits, source fusion,
diversity, safety thresholds, and search deadlines are fixed algorithm or safety
policy rather than operator settings. Learned feature weights change only through
held-out model promotion or rollback.

**Retention.** Cached fallback responses are held for `YAGO_WEB_FALLBACK_CACHE_TTL`
(the only outbound-search cache) and then discarded, bounding how long external
result text lingers. The local index-result cache is a fixed 16 MiB/256-entry LRU,
and public paging sessions share a fixed 32 MiB/128-session LRU with a five-minute
TTL. Both caches count detached payload bytes, evict by bytes and recency, and do
not retain a single entry larger than their budget. These fixed safety ceilings
do not add environment-only controls. Query logs are emitted to the node's
structured log stream, so their retention is governed by the operator's log
pipeline rather than the node. Retention windows for stored document snippets
and crawl logs follow the storage eviction settings under **Crawling** and are
not yet independently tunable.

**Fixed safety ceilings.** Tavily-compatible JSON bodies are limited to 64 KiB.
Raw-content search, extract, crawl, and map share four work slots, a 30-second
request deadline, and 16 MiB retained/output budgets; one live HTML fetch is
hard-capped at 4 MiB. YaCy blacklist and profile exports each admit four
requests before parsing. Blacklist input and output share 16 MiB; profile input,
owned properties, and output are capped at 1 MiB, 1 MiB, and 2 MiB. These are
process safety invariants rather than operator tuning knobs.

When a Tavily-compatible request sets `include_usage`, the success envelope adds
request-local compatible units derived from completed work. Executed basic,
fast, and ultra-fast searches use one unit; an executed advanced search uses
two. Extract counts each complete group of five successful results, doubled for
advanced depth. Map counts each complete group of ten successful pages, doubled
when instructions are present. Crawl adds those mapping units to extraction
units from complete groups of five successful pages. Failed items do not count,
and a `max_results:0` search that does not execute retrieval reports zero. These
units are compatibility accounting, not billing, an account balance,
external-provider spend, or proof of an upstream Tavily call. No Admin setting
or environment variable controls the formulas.

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
An active session receives a new unpredictable cookie token after the earlier of
one hour or half of its configured lifetime. Rotation preserves the CSRF token and
the original absolute expiry, atomically invalidates the replaced token, and fails
closed when the replacement cannot be persisted. This is a fixed security policy,
not an operator setting.
Login and API-key outcomes are exported on `/metrics` as
`admin_login_attempts_total` (results `success`, `failure`, `throttled`) and
`admin_api_key_auth_total` (results `rejected`, `throttled`, `forbidden`) so
operators can alert on brute-force pressure.

The JSON login and setup endpoints require `Content-Type: application/json`
(an optional charset is accepted). The browser setup page issues a short-lived,
signed token in both its host-only `HttpOnly`, `SameSite=Strict` cookie and its
hidden form field; setup rejects a missing, altered, expired, or cross-site token
and clears the cookie after the attempt. Dynamic login, setup, and admin pages are
served as `private, no-store`. Auth pages load only same-origin static CSS and
icons under a policy that disables every other content source and forbids framing.

The browser login page leaves the account name empty and displays only bounded
public node facts: node name, the advertised swarm endpoint, processor model and
logical processor count (with architecture as fallback), total and free memory,
free space on the filesystem
holding `YAGO_DATA_DIR`, YaGo version, and node uptime. A failed individual
system read is shown as `Unavailable`; the page does not expose account names,
private listener addresses, or the configured data path.

The login and setup stylesheet URL carries the current embedded content digest.
That exact revision is cacheable as immutable, while the canonical unversioned
URL revalidates. A wrong, duplicate, extra, encoded, or noncanonical revision
request returns `404` with `Cache-Control: private, no-store`.

Login and setup accept at most 16 KiB per JSON or form body, a 256-byte
username, and a 1 KiB password. One 32-slot process gate covers unauthenticated
login/setup body decoding across the JSON and HTML surfaces; saturation returns
`503` with `Retry-After` before reading the body. Argon2 hashing and verification
use a separate two-slot process gate. Login tracking retains at most 4,096
client identities and 64 failures per identity.

For non-interactive clients, create an API key with
`POST /api/admin/v1/auth/api-keys` (a session or an `admin:write` key is required
to manage keys). The response returns the secret exactly once; the node stores
only its SHA-256 hash alongside a public identifier. Send the key as
`Authorization: Bearer <key>` instead of a cookie; it is checked against the scope
the path requires, is rate limited per key, and needs no CSRF token. Scopes are
`admin:read` (read-only operations such as `/metrics`), `admin:write` (state
changes and key management), `crawl:write` (`POST /crawl`), and `search:read` /
`search:raw`, which always gate Admin-minted keys on the public
Tavily-compatible search API. `YAGO_SEARCH_REQUIRE_API_KEY` makes that surface
scoped-only by disabling the legacy static token. `GET /api/admin/v1/auth/api-keys` lists key
metadata and last-used time without the secret. It uses bounded keyset paging:
`cursor` is the exclusive identifier of the last key from the preceding page,
and `limit` defaults to 256 and accepts values from 1 through 256. When
continuation is required, or when `cursor` or `limit` is supplied explicitly,
the response reports the full store `total`; `nextCursor` and `truncated`
identify another page. A default request against a store of at most 256 keys
retains the earlier keys-only response shape. Legacy stores above the current
256-key creation cap remain readable and can be drained with paged `GET` and
`DELETE /api/admin/v1/auth/api-keys/{id}` requests; the cap blocks new creation
rather than hiding existing records. Admin → Security uses 20-row pages with
Previous/Next navigation, retains at most 256 validated prior cursors, and keeps
a recovery path when deletion leaves a cursor on an empty page. API-key lookup
and last-used persistence share a 32-slot process-wide nonblocking gate.
Saturation or authentication storage failure returns `503` with `Retry-After: 1`;
per-key throttling returns `429` with the same header. Last-used time is written
only after the request passes the per-key limiter. Prometheus
can scrape `/metrics` with an `admin:read` key or a logged-in session cookie;
otherwise bind `YAGO_OPS_ADDR` to a trusted network. `GET
/api/admin/v1/events` returns a bounded, in-memory log of recent structured
events (severity and category, newest first, optional `limit`) under the same
`admin:read` scope. A bounded asynchronous writer persists operator-worthy
events and reseeds the in-memory ring at startup, so recent events survive a
restart without putting event storage on request or crawl-progress paths.

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
admin surface can be kept off the public network. Bind `YAGO_OPS_ADDR` to loopback
(for example `127.0.0.1:9090`) or a private interface and reach it through a
trusted reverse proxy, while `YAGO_PEER_ADDR` stays on the public interface for
P2P. Terminate TLS at that proxy so the session cookie is marked `Secure`.

## Crawling

The node can drive a crawl fleet over gRPC: it serves a `CrawlExchange` endpoint that crawlers dial. Operators start a crawl by posting seed URLs to `/crawl` on the ops address; the node enqueues orders in a durable, store-backed queue and streams them to connected crawlers, and crawled pages flow back in as bounded ingest batches. Local identity and encoded-size validation stops an invalid payload before RPC. A node-returned `Unavailable` or legacy `ResourceExhausted` saturation status retries with jittered exponential delay capped at five seconds until the crawl context ends. The endpoint defaults to loopback at `127.0.0.1:9091` for a co-located crawler; set `YAGO_CRAWL_RPC_ADDR=off` for a pure peer or an explicit bind such as `:9091` for remote workers.

### Extraction-generation refresh

Newly parsed documents carry extraction generation `1`; stored payloads that
predate the field read as generation `0`. When a material parser or extractor
change advances the current generation, Admin → Index offers an explicit action
that examines an operator-selected 1–100 raw storage records per submission
(default 20) and queues only documents whose generation is missing or older.
Current or newer documents are skipped. The action is available only when the
document reader and crawler dispatch are configured.

One pass captures the high key of the legacy and admission-ordered document
partitions, then carries those ends and its current position in a bounded
continuation token. This prevents continuous ingest from extending the pass, but
it is not a transactional snapshot: later submissions may observe intervening
replacement or deletion, and admissions beyond the captured ends wait for a new
pass. Each stale batch enters the existing durable crawl-dispatch queue with an
idempotency identity for that action and continuation. A delivery failure keeps
the same position available for retry.

This is an operator action, not a runtime setting. It has no environment variable
and never starts automatically after an upgrade, so changing an extraction
generation cannot create an unbounded recrawl storm.

Automatic swarm crawls are enabled by default at depth 5 and 250 pages per
task. Web-discovery crawling remains disabled until the operator enables it;
its ready profile uses the same depth and whole-run page cap. Both automatic paths crawl
query-bearing URLs, accept untrusted TLS certificate authorities, use the HTTP
fast path without browser rendering, honor robots.txt, skip links marked
`rel=nofollow`, and schedule indexed pages for refresh after 30 days. These
values are editable under Admin → Configuration → Crawler; runtime overrides
take precedence over the environment-derived defaults. That tab contains
separate Crawler, Automatic discovery, and Document formats fieldsets. The
legacy `/admin/autocrawler` URLs redirect to this tab and do not keep a second
settings surface.

When web-fallback crawl seeding is enabled, every eligible surfaced URL enters
the bounded background warming path described by `YAGO_WEB_FALLBACK_SEED_CRAWL`.
Fragments are removed before URL deduplication; credential-bearing, addressless,
non-HTTP, and oversized identities are rejected. Each absent or
lookup-indeterminate URL attempts one URL-idempotent durable
automatic-discovery publication and keeps the web-discovery profile's depth,
per-host limit, and whole-run cap. At most one accepted order remains for a
normalized URL. Its root page is the warming fetch, so the node does not create a
second cache order for the same result.

`YAGO_CRAWLER_MAX_PAGES_PER_RUN` bootstraps a 50,000-page whole-run budget in
both the node and crawler. The node records the effective value in every new
manual and scheduled profile. A swarm- or web-discovery task uses the smaller
of its dedicated automatic-task cap and this global cap when the global value
is positive; global `0` never removes the dedicated automatic cap. Recrawls
retain the profile value under which the URL was first scheduled.
Configuration → Crawler changes the default live for subsequent tasks, and the
manual crawl form can override it per task. A value of `0` is deliberately
unlimited for profiles that do not carry a separate dedicated cap.
Manual and recrawl profiles retain the whole-run value recorded in the
profile. Legacy automatic-discovery profiles that predate the whole-run field
derive that limit from their stored positive per-host cap during recovery, so
an old automatic task cannot expand to the crawler bootstrap default. If its
persistent frontier already contains more work than the derived cap permits,
recovery removes the newest pending pages in bounded atomic batches and keeps
the oldest pending work. Completed totals, visited history, and per-host
admission facts remain unchanged. Durable discarded-page accounting makes the
operation idempotent across repeated crashes; when completed work already meets
the cap, recovery removes every pending page and settles the task successfully.

Automatic-discovery orders carry explicit priority metadata. With
`YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY=true`, the durable queue selects at
most three discovery orders before a waiting normal order, and each crawler
dispatches at most three due discovery pages before a due normal page. Existing
run fairness and value scoring select work within each class. With priority
disabled, both durable priority classes use one shared global FIFO sequence and the crawler
uses its existing run-fair, value-scored page selection across both classes.
Priority and durable fairness state survive requeue, lease expiry, and node
restart.

Pending JSON payloads remain in the established `crawlorders` bucket. Secondary
priority indexes contain only keys. A downgraded node ignores those indexes but
still drains every order in global FIFO order; the current node reconciles the
tail admitted during the downgrade and removes stale keys after it returns.

The node and crawler environments must bootstrap the same priority value. The
crawler completes one heartbeat attempt bounded to one second before opening its order stream;
a successful response applies the persisted node value before intake. If that
attempt fails, the environment bootstrap is the only available policy until a
periodic heartbeat succeeds and applies the authoritative node value live.

`crawler.max_active_runs` is the per-process active-task workbench. It defaults
to 32, accepts 1–256, and is independent of `crawler.fetch_workers`. A task holds
one slot from prepared-order admission through terminal completion; additional
ordinary or recovered tasks wait without activating another frontier or periodic
progress reporter. Increasing the value wakes waiting work. Decreasing it lets
already active tasks finish and admits no replacement until occupancy is below
the new limit. The environment bootstrap and Admin value must be configured in
the node and crawler together.

On reconnect, the node declares the complete adopted-lease manifest once and
streams it in confirmed batches of at most 16. Periodic heartbeats keep every
active lease alive but do not confirm unseen deliveries; a targeted heartbeat
must explicitly confirm the current lease or batch. This protocol is automatic
and has no separate operator timing control.

Every order-stream attempt carries its own cancellation through lease
confirmation and local active-run admission. If a node restart invalidates the
process session while a confirmed delivery is waiting locally, that attempt is
cancelled and the live crawler opens a replacement stream to adopt its durable
leases. Restarting the crawler instead preserves its checkpoint-backed worker
identity, creates a new process session, and adopts the same worker's unfinished
leases. No runtime setting controls this recovery behavior.

Accepting an untrusted certificate authority preserves encryption but does not
authenticate the remote server and permits an on-path endpoint to substitute
content. Disable this option when indexing requires verified server identity.

The `/crawl` request body accepts `seeds` and optional `startMode`. Supported
start modes are `url`, `sitemap`, `sitelist`, and `robots`; empty mode is treated
as `url`. Sitemap and sitelist starts are expanded by the crawler into bounded URL
roots before normal frontier admission. A `robots` start reads each seed host's
`robots.txt`, expands the sitemaps named in its `Sitemap:` directives, and admits
those URLs. A 404 or 410 discovers nothing; transient fetch failures requeue the
leased order. Invalid order input and malformed sitemap content terminate without
a poison retry loop.

| Variable | Default | Description |
| --- | --- | --- |
| `YAGO_CRAWL_RPC_ADDR` | `127.0.0.1:9091` | Address the node serves the crawl gRPC endpoint on. `off` disables crawling; use an explicit bind such as `:9091` for remote workers. Dedicated Admin key: `bind.crawler`; Disable persists the off state, and Reset deletes the override so this environment value is authoritative again. Changes take effect after restart. |
| `YAGO_CRAWLER_NODE_RPC_ADDR` | _(required)_ | Node crawl-gRPC address used by the crawler process. Use `127.0.0.1:9091` for the packaged same-host services and `yago-node:9091` in Compose. This deployment topology input is not a runtime crawler policy. |
| `YAGO_CRAWLER_WORKER_ID` | `yago-crawler` | Optional one-line display prefix of at most 219 UTF-8 bytes for the crawler's stable checkpoint-backed worker identity. The checkpoint appends `-<UUID>` within the 256-byte protocol limit. This process identity input is not a runtime crawler policy. |
| `YAGO_CRAWLER_METRICS_ADDR` | _(empty)_ | Optional loopback IP-literal crawler Prometheus listener, for example `127.0.0.1:9101` or `[::1]:9101`; empty starts no crawler HTTP listener. Wildcard and non-loopback listeners are rejected; expose a remote scrape only through a trusted tunnel or proxy. Admin key: `crawler.metrics_address`. Keep the same bootstrap in both services; the node delivers the persisted value before crawler assembly and a change gracefully restarts connected crawlers. |
| `YAGO_CRAWLER_BROWSER_PATH` | _(PATH discovery)_ | Optional absolute clean path whose basename is exactly `firefox` or `firefox-esr`. Empty discovers either launcher through `PATH`; no installed Firefox leaves the HTTP fast path available and fails only a requested browser fallback. A configured or discovered launcher must resolve through a root-owned path chain that is not group- or other-writable to a regular, non-set-ID executable available to the crawler identity; this is checked before browser assembly and again before each spawn. Admin key: `crawler.browser_path`. Keep the same bootstrap in both services; the node delivers the persisted value before crawler assembly and a change gracefully restarts connected crawlers. |
| `YAGO_CRAWLER_WORKERS` | `4` | Bootstrap page-fetch concurrency for each connected crawler process (1–256). Keep the same bootstrap value in the crawler environment. The persisted Configuration → Crawler value is sent over heartbeat and becomes authoritative after connection; it limits neither crawl runs nor queued tasks. |
| `YAGO_CRAWLER_MAX_PAGES_PER_SECOND` | `10` | Bootstrap fleet-wide page-fetch start rate (0–1,000,000; `0` is unlimited). Admin key: `crawler.max_pages_per_second`, shown as “Maximum fleet-wide fetch-start rate”. The node leases non-bursting relative start windows across all connected crawler processes and active runs. The crawler measures the previous completed lease RPC, caps its delivery allowance at one second, and reports it on the next sequence. The node widens that demand-backed batch and reserves its complete final window before another batch. The crawler intersects openings with response receipt and closings with request send, then enforces one configured interval between permits actually used. A latency spike beyond the preceding allowance can discard a permit but cannot produce a catch-up burst. Each crawler also uses the value as a local process smoother; page-fetch workers, per-run pace, and per-host politeness remain additional limits. A live change is authoritative after heartbeat. Enabling or reducing a finite ceiling fences existing order streams before new-rate permits are issued; increasing it retains capable sessions. A finite rate fails closed against a node or crawler without fetch-start lease support. |
| `YAGO_CRAWLER_MAX_REDIRECTS` | `10` | Bootstrap redirect-hop maximum for guarded HTTP and browser fetches (0–1,000; `0` rejects the first redirect). Admin key: `crawler.max_redirects`. It applies live after heartbeat; HTTP clients read it immediately and Firefox sessions lazily relaunch before the next render. Keep the same bootstrap value in the crawler environment. |
| `YAGO_CRAWLER_MAX_ACTIVE_RUNS` | `32` | Bootstrap active crawl-task limit for each connected crawler process (1–256). Admin key: `crawler.max_active_runs`; it applies live after heartbeat and is independent of page-fetch workers. Excess ordinary and recovered tasks wait without activating frontier or progress work. |
| `YAGO_CRAWLER_FRONTIER_STATE_MAX_BYTES` | `4GB` | Bootstrap in both services for the soft physical boundary on `crawler/frontier-v1.db`; `0` disables it. Admin key: `crawler.frontier_state_max_bytes`; it applies live after heartbeat and wakes fresh orders waiting at the previous boundary. |
| `YAGO_CRAWLER_MAX_PAGES_PER_RUN` | `50000` | Bootstrap whole-run page budget in both node and crawler environments. The node stamps the current Configuration → Crawler value into each new manual and scheduled profile. For swarm and web discovery, a positive value can only reduce the dedicated automatic-task cap; `0` leaves that dedicated cap intact. Existing manual and recrawl profiles retain their recorded value. Recovered legacy automatic profiles that omit the whole-run field derive it from their stored positive per-host cap. |
| `YAGO_CRAWLER_PRIORITIZE_AUTOMATIC_DISCOVERY` | `true` | Bootstrap in both node and crawler environments. Gives explicit swarm and web-discovery work bounded three-order and three-page priority. `false` preserves exact global FIFO order leasing and disables class preference in crawler page dispatch. The one-second startup heartbeat is authoritative when successful; later heartbeat changes apply live. |
| `YAGO_CRAWLER_ALLOW_PRIVATE_NETWORKS` | `false` | Bootstrap opt-in for crawler access to RFC 1918 and IPv6 ULA targets. Admin key: `crawler.allow_private_networks`. Loopback, link-local, metadata, carrier-grade NAT, multicast, and reserved ranges remain blocked. |
| `YAGO_CRAWLER_ALLOW_CIDRS` | _(empty)_ | Comma-separated private CIDRs admitted without opening all private space, limited to 64 RFC 1918 or IPv6 ULA subnets. Admin key: `crawler.allow_cidrs`. It cannot widen access to loopback, link-local, metadata, carrier-grade NAT, multicast, or reserved ranges. |
| `YAGO_CRAWLER_BROWSER_SANDBOX` | `false` | Bootstrap Firefox content-process sandbox policy in both service environments. Admin key: `crawler.browser_sandbox`. A sandbox-only live change lets an active render finish, then retires every pooled Firefox session before that slot's next render. Enable it only when the deployment permits Firefox's required unprivileged namespaces. |
| `YAGO_CRAWLER_BROWSER_FAILURE_THRESHOLD` | `5` | Consecutive browser launch or render/navigation failures before the slow-path circuit opens (0–1,000; `0` disables the circuit breaker so every browser-eligible fetch may attempt the browser path). Admin key: `crawler.browser_failure_threshold`. |
| `YAGO_CRAWLER_CONNECT_TIMEOUT` | `5s` | Origin TCP connection timeout from 1 millisecond through 2 minutes. Admin key: `crawler.connect_timeout`. |
| `YAGO_CRAWLER_CRAWL_DELAY` | `1s` | Default same-host delay from zero through 1 hour when a crawl profile or robots policy does not require a larger delay. Admin key: `crawler.crawl_delay`. |
| `YAGO_CRAWLER_HEADER_TIMEOUT` | `10s` | Origin response-header timeout from 1 millisecond through 2 minutes. Admin key: `crawler.header_timeout`. |
| `YAGO_CRAWLER_MAX_DEPTH` | `5` | Hard execution ceiling for link depth in every crawl profile (1–64). The default preserves the shipped depth-five automatic-discovery profiles. Admin key: `crawler.max_depth`. |
| `YAGO_CRAWLER_MAX_HOST_CONCURRENCY` | `2` | Maximum concurrent fetches to one host in each crawler process (1–256); crawl delay remains an additional politeness limit. Admin key: `crawler.max_host_concurrency`. |
| `YAGO_CRAWLER_REQUEST_TIMEOUT` | `15s` | Whole-request deadline from 1 millisecond through 10 minutes, covering connection, redirects, headers, and response body. Admin key: `crawler.request_timeout`. |
| `YAGO_CRAWLER_RUN_PAGES_PER_MINUTE` | `30` | Default fetch-start pace for each crawl run (0–1,000,000; `0` is unlimited). Admin key: `crawler.run_pages_per_minute`; an explicit per-run control still overrides the default. |
| `YAGO_CRAWLER_SITEMAP_URL_LIMIT` | `10000` | Maximum URLs admitted from one sitemap, sitemap index, robots sitemap set, or sitelist expansion (1–1,000,000). Admin key: `crawler.sitemap_url_limit`. |
| `YAGO_CRAWLER_TLS_TIMEOUT` | `5s` | Origin TLS-handshake timeout from 1 millisecond through 2 minutes. Admin key: `crawler.tls_timeout`. |
| `YAGO_CRAWLER_SHUTDOWN_GRACE` | `10s` | Drain deadline from 1 millisecond through 5 minutes for fetch workers and final progress delivery during stop or policy restart. Admin key: `crawler.shutdown_grace`. |
| `YAGO_CRAWLER_USER_AGENT` | `yago-crawler/<version> (+https://github.com/D4rk4/yago/)` | One-line HTTP identity of at most 256 bytes for page, robots, sitemap, and browser fetches. Admin key: `crawler.http.user_agent`; an empty environment value selects the versioned default. |

The node and crawler environments bootstrap the same values for these eighteen
runtime-policy controls. The node's persisted Configuration → Crawler values
are authoritative: a crawler reads the typed policy before constructing its
fetch stack. A sandbox-only heartbeat change applies to the browser pool without
restarting the crawler, and a frontier-state boundary change updates admission
and wakes waiters live; every other policy change triggers a graceful automatic
crawler restart. Optional fields 15–18 carry sandbox, browser path, metrics
address, and `frontier_state_max_bytes`; startup-envelope fields 19 and 20 carry
the crawler storage reserve and hysteresis. Omission preserves the crawler's
current or environment-bootstrap value, while a current node sends all six
explicitly, including zero, and is authoritative. A crawler talking to an older
node that does not implement policy delivery retains its environment bootstrap.

A worker-count change pauses new page intake in each connected crawler, lets its
active page fetches finish, and starts the latest requested worker group without
restarting the process. The value applies independently to every crawler
process; aggregate fleet concurrency is therefore this value multiplied by the
number of connected processes.

An aggregate-fetch-rate change wakes current budget waiters and applies the new
spacing to later fetch starts. It does not cancel in-flight requests. Per-run
rate controls and per-host politeness can only reduce the resulting rate.

An active-task-limit change does not resize page-fetch workers and does not
cancel active tasks. It changes only subsequent task admission in each crawler
process. Aggregate fleet task capacity is the per-process value multiplied by
the number of connected crawler processes.
