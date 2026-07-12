# Configuration

The node is configured through environment variables.

> **Deprecation (yacy → yago rename):** the node variables below were renamed
> from the `YACY_` prefix to `YAGO_`. For one release, an unset `YAGO_` variable
> falls back to its legacy `YACY_` name and logs a one-time deprecation warning;
> migrate to the `YAGO_` names. The crawler fleet worker's variables were renamed
> from `YACYCRAWLER_` to `YAGOCRAWLER_` without a fallback.

## Runtime overrides

A small, whitelisted set of settings can also be changed at runtime from the
admin console's Configuration section. An override is stored durably in the node
vault and takes precedence over the environment default; clearing it (**Reset to
default**) reverts to the environment value. Overrides survive restarts, require
an authenticated admin session with a CSRF token, and record a `config` event on
each change. Secrets are never runtime-overridable. The public search portal
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
always returns an empty completion list. This is separate from the
YaCy-compatible `/opensearchdescription.xml`, which describes the `/yacysearch.*`
endpoints. The **HTTP→HTTPS
redirect** toggle overrides `YAGO_HTTPS_REDIRECT` (off by default): when on, a
plain-HTTP request is answered with a 308 to the `https://` origin, preserving
the path and query. TLS is expected to be terminated in front (a reverse proxy
sets `X-Forwarded-Proto`); loopback requests are never redirected, so the admin
console reached over `localhost` cannot be pushed to an unreachable HTTPS origin.

### Listen addresses

The node runs three separate HTTP listeners, each with a distinct job:

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

The Configuration section also has a per-surface bind editor. It lists the host's
network interface addresses (including loopback) and lets you set, per listener,
the interface (or **all interfaces**) and the port for each of the three surfaces
above. A bind override is validated — the host must be one of the machine's own
interface addresses, so you cannot bind a surface to an unreachable address —
persisted durably, and applied on the next restart. Loopback and all-interfaces
are always offered so a bind change cannot lock you out of the admin console.
Changing the **peer** listener's port also re-derives the advertised port and the
local DHT self-test address from the new value, so the node announces the port it
actually listens on — unless you pinned them explicitly with `YAGO_ADVERTISE_PORT`
or `YAGO_PUBLIC_SELF_TEST_URL`, which stay authoritative (set the latter when the
peer listener is behind a reverse proxy or NAT and the external address differs).

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `INFO` | Log verbosity: `DEBUG`, `INFO`, `WARN`, or `ERROR`. |
| `YAGO_PEER_BIRTH_DATE` | first start time | Optional peer birth date (`YYYYMMDD`, UTC) stored on the first start with a fresh data directory. Declare it when migrating an established peer identity or when private-network tests need an aged peer; YaCy peers skip DHT targets younger than three days. |
| `YAGO_DATA_DIR` | `./data` | Where the node persists its data. New nodes use `yago-node.db`; existing `yacy-rwi.db` files are opened when no `yago-node.db` exists. The embedded full-text fallback index is `search.bleve` inside this directory. The YaCy-compatible peer profile file is `SETTINGS/profile.txt` inside this directory. Shared blacklist export reads `SETTINGS/yacy.conf` `BlackLists.Shared` and the referenced files under `LISTS/`. |
| `YAGO_PEER_ADDR` | `:8090` | Listen address for the YaCy peer protocol. |
| `YAGO_OPS_ADDR` | `:9090` | Listen address for `/health`, `/ready`, `/metrics`, and ops JSON endpoints. |
| `YAGO_METRICS_ENABLED` | `true` | Serve the Prometheus `/metrics` endpoint. Set to `false` to unmount it (returns 404); the collectors still run. The endpoint is admin-authenticated regardless (it is not on the operations listener's public allowlist). See [metrics.md](metrics.md). |
| `YAGO_ADMIN_RESTART_ENABLED` | `true` | Offer the node/crawler restart controls in the admin console. Set to `false` to strip them: the Restart page then renders as unavailable (UI-09 acceptance). The setup wizard's mandatory post-setup restart is unaffected. |
| `YAGO_PEER_HASH` | _(generated)_ | The 12-character enhanced-Base64 peer hash advertised to the network. If unset, the node generates one on first start and persists it to the data directory, reusing it across restarts so the peer keeps a stable identity. Set it to pin a specific hash. |
| `YAGO_PEER_NAME` | _(generated)_ | Peer name advertised to the network. Generated (as `yago-<random>`) and persisted like the hash when unset. |
| `YAGO_NETWORK_NAME` | `freeworld` | YaCy network to join. Only peers on the same network exchange data. |
| `YAGO_SEEDLIST_URLS` | _(empty)_ | Comma-separated YaCy seedlist URLs to discover peers from. |
| `YAGO_ADVERTISE_HOST` | _(auto)_ | Public IP or DNS name other peers use to reach you. When unset and the node announces to the network (`YAGO_SEEDLIST_URLS` set), it auto-detects the first non-loopback IPv4 address. Set it explicitly behind NAT or Docker bridge networking, where the guess is wrong; the DHT self-test demotes an unreachable self. |
| `YAGO_ADVERTISE_PORT` | _(the `YAGO_PEER_ADDR` port)_ | Port other peers use to reach you. |
| `YAGO_PUBLIC_SELF_TEST_URL` | local peer URL | Base URL used by outbound DHT gates to self-test `/yacy/query.html?object=rwicount`. Set it to the externally reachable peer URL when the local listener is behind a reverse proxy or NAT. |
| `YAGO_ANNOUNCE_INTERVAL` | `10m` | How often to re-announce yourself to the network (e.g. `30s`, `10m`, `1h`). |
| `YAGO_GREETS_PER_CYCLE` | `16` | How many peers to greet in each announce cycle. |
| `YAGO_NETWORK_DHT` | `true` | Enables the sender-side DHT gate equivalent to YaCy `network.unit.dht`. |
| `YAGO_DHT_DISTRIBUTION` | `true` | Enables the sender-side DHT distribution gate equivalent to YaCy `allowDistributeIndex`. |
| `YAGO_DHT_ALLOW_WHILE_CRAWLING` | `false` | Allows outbound DHT distribution while local crawling is active. |
| `YAGO_DHT_ALLOW_WHILE_INDEXING` | `true` | Allows outbound DHT distribution while local indexing is active. |
| `YAGO_DHT_DISTRIBUTION_INTERVAL` | `10s` | How often the outbound DHT scheduler runs a distribution cycle. |
| `YAGO_DHT_REDUNDANCY` | `3` | Number of DHT target peers per vertical partition for outbound transfer and global remote search. Accepted values are `1` through `16`; the default matches YaCy freeworld senior peers. |
| `YAGO_DHT_PARTITION_EXPONENT` | `4` | YaCy vertical DHT partition exponent. Accepted values are `0` through `8`; the default creates 16 vertical partitions, matching YaCy freeworld. |
| `YAGO_DHT_MINIMUM_PEER_AGE_DAYS` | `3` | Minimum peer age for DHT target eligibility. Set `-1` only for controlled tests or private networks that intentionally disable the age gate. |
| `YAGO_DHT_MINIMUM_CONNECTED_PEERS` | `33` | Sender-side DHT gate: minimum connected peers before outbound distribution starts. Lower it only for private networks or controlled tests. |
| `YAGO_DHT_MINIMUM_RWI_WORDS` | `100` | Sender-side DHT gate: minimum locally stored RWI words before outbound distribution starts. |
| `YAGO_TRUSTED_PROXIES` | _(empty)_ | Comma-separated CIDRs or IPs of reverse proxies fronting the node. Set this when running behind a reverse proxy so peers are not told the proxy's address. |
| `YAGO_STORAGE_QUOTA` | `1GB` | Storage quota, as a human-readable size (e.g. `512MB`, `1GB`, `20GB`). |
| `YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS` | `false` | Allow outbound connections to RFC 1918 and unique-local addresses. Enable only for LAN or private-network deployments; loopback, link-local, and reserved ranges stay blocked. |
| `YAGO_EGRESS_ALLOW_CIDRS` | _(empty)_ | Comma-separated private CIDRs the egress guard admits even when `YAGO_EGRESS_ALLOW_PRIVATE_NETWORKS` is false, so intranet mode reaches only named ranges instead of all private space. Only relaxes the private check: loopback, link-local (including the cloud metadata range), and reserved ranges stay blocked, so a non-private entry never grants access to them. |
| `YAGO_SEARCH_API_KEY` | _(empty)_ | Optional legacy static bearer token for the Tavily-compatible `POST /search`, `POST /extract`, `POST /crawl`, and `POST /map` endpoints. Callers send `Authorization: Bearer <token>`. This local node credential is not a key for an external search service; the node uses no keyed external search API. Ignored when `YAGO_SEARCH_REQUIRE_API_KEY` is on. When neither this token nor scoped authorization is configured, the agent endpoints deny access. |
| `YAGO_SEARCH_REQUIRE_API_KEY` | `false` | Require scoped API keys on the Tavily-compatible surface. When on, `POST /search`, `POST /extract`, `POST /crawl`, and `POST /map` accept only admin-minted API keys (`Authorization: Bearer <key>`): ordinary `/search` needs `search:read`, while raw-content search, extract, crawl, and map need `search:raw`. Missing/invalid keys return `401`, insufficient scope `403`, and rate-limited keys `429`. Takes precedence over `YAGO_SEARCH_API_KEY`; when neither scoped authorization nor a static key is configured, these endpoints deny access rather than becoming public. |
| `YAGO_PUBLIC_SEARCH_UI_ENABLED` | `false` | Serve the anonymous public search portal on the public listener's root (`/`). Off by default; while off, the root serves the landing page and the portal is not mounted. When on, a minimal, server-rendered, no-JavaScript search page runs exact/morphological retrieval against the local index plus YaCy peers and bounded local fuzzy recovery. Operator-permitted web search runs after a miss or in parallel according to `YAGO_WEB_FALLBACK_TRIGGER`. It exposes only search — never admin APIs — and does not log the query text. Overridable live from the admin console (see Runtime overrides). |
| `YAGO_HTTPS_REDIRECT` | `false` | Redirect plain-HTTP requests to the `https://` origin with a 308, preserving path and query. Off by default. TLS termination is expected in front (a reverse proxy sets `X-Forwarded-Proto`); loopback requests are never redirected. Overridable live from the admin console (see Runtime overrides). |
| `YAGO_EXTRACT_FETCH_ENABLED` | `false` | Enable fetch-on-extract for `POST /extract`. Off by default, so an uncached URL is a controlled `failed_result` with no outbound request. When on, an uncached URL is fetched through the shared egress-guarded client (private networks stay default-denied — no SSRF) and its title and visible text are extracted. |
| `YAGO_EXTRACT_FETCH_TIMEOUT` | `10s` | Per-request timeout for a fetch-on-extract fetch. |
| `YAGO_EXTRACT_FETCH_MAX_BYTES` | `2097152` | Maximum response bytes read per fetch-on-extract fetch. The default is 2 MiB and the process clamps larger values to a 4 MiB hard ceiling before HTML parsing. An over-limit response is rejected, not truncated into a partial document. |
| `YAGO_ADMIN_USER` | _(empty)_ | Administrator username. When set with `YAGO_ADMIN_PASSWORD`, the admin is provisioned on every start and those credentials are authoritative. |
| `YAGO_ADMIN_PASSWORD` | _(empty)_ | Administrator password, stored as an Argon2id hash. Leave both admin variables empty to create the first admin with `POST /api/admin/v1/auth/setup` on first run. There is no default password. |
| `YAGO_ADMIN_CORS_ORIGINS` | _(empty)_ | Comma-separated origin allowlist for cross-origin browser requests to the operations surface. Empty denies all cross-origin requests. Use `*` to echo any origin (required for a credentialed admin UI on an unknown origin, but broad). |
| `YAGO_SEARCH_CORS_ORIGINS` | _(empty)_ | Comma-separated origin allowlist for cross-origin browser requests to the public search endpoints on the public listener. Empty denies all cross-origin requests; `*` allows any origin without credentials. |
| `YAGO_SEARCH_REMOTE_PEER_TIMEOUT` | `1200ms` | Maximum contribution time for one YaCy peer inside an interactive search. |
| `YAGO_SEARCH_REMOTE_TIMEOUT` | `1300ms` | Aggregate YaCy peer fan-out budget. The public pipeline enforces a 1.8-second end-to-end deadline; a request permitted to continue to web fallback caps its complete exact local-plus-swarm and peer-evidence stage at 600ms so later recovery stages retain time. One query additionally shares fixed limits of 8 MiB response data, 1,024 metadata rows, and 8,192 abstract hashes across exact and morphology passes; at most 32 peer HTTP attempts run process-wide. These limits preserve partial swarm results and do not penalize a peer when local admission is saturated. |
| `YAGO_WEB_FALLBACK_ENABLED` | `false` | Legacy on/off switch for optional DDGS web search, kept for compatibility. It is the default source for `YAGO_WEB_FALLBACK_PRIVACY` when that variable is unset (`true` -> `enabled`, `false` -> `disabled`). Prefer setting the privacy mode directly. The separate start trigger defaults to miss-only behavior. A local-only request never leaves the node. |
| `YAGO_WEB_FALLBACK_PRIVACY` | _(from `ENABLED`)_ | Governs whether an eligible query may leave the node for the external provider. `disabled` never sends a query and does not install web search; `explicit` sends only for a request that opted in; `enabled` permits automatic external search. A Tavily-compatible `/search` call opts in by its web-search contract, including at basic depth. YaCy `resource=local` and admin `scope=local` never use the provider. Defaults to `disabled` unless legacy `YAGO_WEB_FALLBACK_ENABLED` is `true`. |
| `YAGO_WEB_FALLBACK_TRIGGER` | `miss` | Chooses when a privacy-permitted web query starts. `miss` preserves the ordered exact local-plus-peer, local fuzzy, then web cascade. `parallel` starts bounded web retrieval alongside local and peer retrieval on every eligible query, then rank-fuses and deduplicates the completed local, peer, and web results. Both values are available as the `Web search timing` Admin UI setting and take effect after restart. |
| `YAGO_WEB_FALLBACK_PROVIDER` | `ddgs` | Selects the fallback provider family. Only the keyless `ddgs` metasearch is available; `YAGO_WEB_FALLBACK_BACKEND` chooses the engine within it. |
| `YAGO_WEB_FALLBACK_BACKEND` | `auto` | Engine selection for the fallback. `auto` starts DuckDuckGo HTML first, then hedges DuckDuckGo Lite, Brave, Mojeek, and Bing at 50ms intervals until one answer survives relevance checks. At most eight engine fetch-and-parse attempts run process-wide. `mojeek`, `bing`, `brave`, or `duckduckgo` restrict the engine set. See `doc/adr/0021-in-house-metasearch-backend.md`. |
| `YAGO_WEB_FALLBACK_MAX_RESULTS` | `10` | Maximum fallback results (1–20). |
| `YAGO_WEB_FALLBACK_TIMEOUT` | `10s` | Per-engine timeout ceiling. Interactive search additionally caps the complete hedged web stage at 950ms inside its 1.8-second deadline. |
| `YAGO_WEB_FALLBACK_SAFESEARCH` | `moderate` | Safe-search preference passed to engines that support it (`strict`, `moderate`, `off`). |
| `YAGO_WEB_FALLBACK_CACHE_TTL` | `5m` | How long to cache a fallback response to respect engine rate limits and reduce repeat egress. Normalized responses share a fixed 4 MiB/256-entry byte-aware cache, retain at most 20 rows per query, and bound each title, URL, and snippet before insertion. |
| `YAGO_WEB_FALLBACK_SEED_CRAWL` | `false` | When on (and crawling is enabled), URLs surfaced by the fallback are published as conservative crawl orders so the next identical query can be answered locally. Publishing runs after the search response through a process-wide two-work admission with a ten-second deadline; saturated admission skips optional seed work. URLs already in the document store are skipped, and the durable queue deduplicates by URL. No effect when crawling is disabled. |
| `YAGO_WEB_FALLBACK_SEED_DEPTH` | `1` | Crawl depth for seeded orders (0–8). Kept shallow to bound amplification. |
| `YAGO_WEB_FALLBACK_SEED_MAX_PAGES` | `20` | Per-host page cap for seeded crawl orders. |
| `YAGO_QUERY_LOG_MODE` | `off` | How much of a search query is written to the node's logs. `off` records nothing; `aggregate` records only the query length and result count (never the text); `full` records the query text. The default keeps queries out of the logs. |
| `YAGO_INDEX_REMOTE_RESULTS` | `true` | Cache the metadata of results returned by peers into the local index after a swarm search, mirroring YaCy's `addResultsToLocalIndex`, so a later query for the same content is answered locally without re-fetching. Metadata only (title, snippet, URL — no crawl); a URL already in the store is never overwritten, so a locally crawled full page is preserved. Writes run off the request path, are limited to two concurrent operations with a 30-second deadline, and are skipped while that bounded admission is saturated. Set to `false` to leave the index unchanged by searches (a node that indexes only what it crawls). |
| `YAGO_PEER_HTTPS_PREFERRED` | `false` | Prefer HTTPS for outbound YaCy peer-protocol calls (hello, remote search, transferRWI/transferURL, back-ping) to peers that advertise an SSL port and the SSL seed flag, mirroring YaCy's `network.unit.protocol.https.preferred`. A failed HTTPS transport attempt retries the same peer over plain HTTP, YaCy-style. Peer certificates in the wild are self-signed, so certificate verification is disabled for the peer-protocol client only — peer authenticity on the YaCy wire comes from protocol-level checks (target hash, network name, hello magic), not PKI — and the egress guard still applies. |
| `YAGO_SEARCH_LINKS_NEW_TAB` | `false` | Open result links on the public portal, admin console search, and /yacysearch.html in a new tab. Off by default per NN/G guidance (opening new tabs breaks the back button and takes control from the user); when on, links carry `rel="noopener noreferrer nofollow"` and an accessible "opens in new tab" indicator (visible ↗ plus screen-reader text). |
| `YAGO_SWARM_SEED_CRAWL` | `false` | Greedy learning (YaCy 1.5): enqueue a conservative, domain-scoped crawl order (depth 1, up to 20 pages per host, idempotent by URL, skipping URLs already stored) for every URL surfaced by swarm search, so the index grows from what the network already answers with. Requires crawler integration; orders respect the crawler's robots and blacklist handling. |

## Privacy

By default, no query is sent to an external web-search provider and no query text
is logged. Global search may still send query terms to YaCy peers; peer
federation and external-provider egress are separate controls.

**Query logging (`YAGO_QUERY_LOG_MODE`).** A single decorator wraps the composed
searcher, so every search surface — the human portal, the YaCy-compatible
endpoints, and the Tavily `POST /search` drop-in — is covered by one setting.
`off` (default) records nothing, `aggregate` records only the query length and
the result count, and `full` records the query text. The aggregate mode never
writes the query text, so it can be used for volume metrics without retaining
what people searched for.

**External web-search egress (`YAGO_WEB_FALLBACK_PRIVACY`).** The node can consult
an external keyless metasearch provider. The provider necessarily receives the
query, and any pages it returns may be queued for this node to crawl (see
`YAGO_WEB_FALLBACK_SEED_CRAWL`). `disabled` (default) never contacts the provider;
`explicit` contacts it only for a request that opted in; `enabled` permits it for
every eligible global query. `YAGO_WEB_FALLBACK_TRIGGER=miss` waits for exact,
peer, and fuzzy retrieval to miss; `parallel` starts the provider alongside local
and peer retrieval and merges all completed rankings. Human search surfaces tag
external rows with `[ddgs]` and state that the provider received the query.

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
`search:raw`, which gate the public Tavily-compatible search API when
`YAGO_SEARCH_REQUIRE_API_KEY` is on. `GET
/api/admin/v1/auth/api-keys` lists key metadata and last-used time without the
secret, and `DELETE /api/admin/v1/auth/api-keys/{id}` revokes a key. API-key
lookup and last-used persistence share a 32-slot process-wide nonblocking gate.
Saturation or authentication storage failure returns `503` with `Retry-After: 1`;
per-key throttling returns `429` with the same header. Last-used time is written
only after the request passes the per-key limiter. Prometheus
can scrape `/metrics` with an `admin:read` key or a logged-in session cookie;
otherwise bind `YAGO_OPS_ADDR` to a trusted network. `GET
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
admin surface can be kept off the public network. Bind `YAGO_OPS_ADDR` to loopback
(for example `127.0.0.1:9090`) or a private interface and reach it through a
trusted reverse proxy, while `YAGO_PEER_ADDR` stays on the public interface for
P2P. Terminate TLS at that proxy so the session cookie is marked `Secure`.

## Crawling

The node can drive a crawl fleet over gRPC: it serves a `CrawlExchange` endpoint that crawlers dial. Operators start a crawl by posting seed URLs to `/crawl` on the ops address; the node enqueues orders in a durable, store-backed FIFO and streams them to connected crawlers, and crawled pages flow back in as ingest batches. Crawling is off until `YAGO_CRAWL_RPC_ADDR` is set; without it the node behaves as a pure peer.

The `/crawl` request body accepts `seeds` and optional `startMode`. Supported
start modes are `url`, `sitemap`, `sitelist`, and `robots`; empty mode is treated
as `url`. Sitemap and sitelist starts are expanded by the crawler into bounded URL
roots before normal frontier admission. A `robots` start reads each seed host's
`robots.txt`, expands the sitemaps named in its `Sitemap:` directives, and admits
those URLs; a missing or unreadable `robots.txt` discovers nothing rather than
failing the crawl.

| Variable | Default | Description |
| --- | --- | --- |
| `YAGO_CRAWL_RPC_ADDR` | _(empty)_ | Address the node serves the crawl gRPC endpoint on (e.g. `:9091`). Empty disables crawling. |
