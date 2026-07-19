# Configuration parity review (CFG-02)

Every environment variable that controls node or crawler behavior needs a
matching Admin surface. The environment value is the bootstrap default; a
stored override wins and is labelled as live or restart-required. This review
is enforced by an executable catalog that discovers runtime environment
constants and literal `os.Getenv` or `os.LookupEnv` reads. A new variable fails
the gate until it is classified and connected to its operator surface.

## Covered by the settings catalog

The persisted Configuration catalog covers these behavior groups:

- Search, portal, OpenSearch public base URL, query logging, result-link
  behavior, remote-result caching, peer snippets, click capture, extract fetch,
  scoped API access, search rate limits, and the live node logging threshold.
- DDGS consent/start mode, backend, result limit, timeout, safe search, cache
  lifetime, and optional web-discovery crawl profile.
- Swarm morphology, seedlists, peer name and advertised host, LAN discovery,
  announcement cadence, peer HTTPS preference, advertised capability flags,
  and DHT participation, distribution, gates, cadence, redundancy, and
  eligibility thresholds.
- Crawler fetch workers, node-owned fleet-wide fetch-start ceiling, redirect limit,
  active-task limit, global run cap, automatic-discovery priority, automatic
  crawl options and format families, and the separate swarm/web depth and
  complete-task caps. A positive global run cap may reduce an automatic task;
  global zero does not remove its dedicated task cap or per-host ceiling.
- Crawler private-network policy, private CIDR exceptions, trusted Firefox
  executable and content sandbox, browser failure threshold, loopback-only
  crawler metrics listener,
  connection/request/TLS/header timeouts, default crawl delay,
  maximum depth and per-host concurrency, default per-run rate, sitemap limit,
  shutdown grace, and HTTP User-Agent. The node delivers one typed policy before
  crawler assembly and again on heartbeats; persisted changes restart connected
  crawler workers gracefully. A sandbox-only change instead retires each pooled
  browser session after its active render and relaunches it before the next one.
- Main-vault quota, reserved-free and recovery hysteresis for node and crawler,
  compaction, automatic shard growth, deferred fsync, read deferral, restart
  control visibility, ingest quality, and egress/CORS/trusted-proxy policy.
- Advertised port, public reachability self-test URL, and DHT partition
  exponent, including their restart requirements and freeworld compatibility
  bounds. The self-test URL uses one bootstrap/Admin canonicalizer and rejects
  credentials, queries, fragments, opaque URLs, control characters, invalid
  hosts or ports, and values longer than 2,048 bytes.
- Controlled-network authentication mode and all secure remote-crawl controls:
  enablement, exact trusted peers, allowed destinations, request rate,
  outstanding leases, lease lifetime, and queue capacity.

The controlled-network secret is not treated like an ordinary readable value.
`YAGO_NETWORK_AUTHENTICATION_SECRET` bootstraps a persisted write-only Admin
override; pages and events expose only whether it is configured. Administrator
credentials and API-key secrets use the guarded setup and Security flows
instead of the generic settings catalog and are never returned after creation.

## Covered by dedicated Admin surfaces

The peer, public, crawler-exchange, and operations listener addresses are edited
by the dedicated Listen addresses form and take effect after restart. The public
and crawler addresses can be persisted as disabled, and Reset deletes any stored
override so the matching environment bootstrap becomes authoritative again.
Administrator setup, password changes, API-key creation/revocation, crawl
dispatch, saved crawl profiles, and YagoRank coefficients likewise use their
bounded domain-specific surfaces rather than duplicate generic settings.

## Canonical environment-only exceptions

The complete canonical environment-only exception allowlist contains exactly
five inputs:

- `YAGO_DATA_DIR` selects each process's durable local namespace and cannot move
  an open database at runtime.
- `YAGO_CRAWLER_WORKER_ID` chooses the durable crawler identity prefix.
- `YAGO_PEER_HASH` and `YAGO_PEER_BIRTH_DATE` establish the durable peer identity.
- `YAGO_CRAWLER_NODE_RPC_ADDR` selects the transport needed to reach the Admin
  policy and cannot be delivered safely over that same transport.

Dedicated listener binds are Admin-backed bootstrap surfaces, not environment-only
exceptions. Peer, public, operations, and crawler-exchange listener addresses map
to the four persisted Listen addresses controls. The executable catalog checks
those bind keys against the actual Admin binding definitions.

Credential bootstraps are also Admin-backed surfaces rather than exceptions:
`YAGO_ADMIN_USER` and `YAGO_ADMIN_PASSWORD` provide authoritative headless
administrator provisioning on each startup; the guarded first-run setup and
password-change flows are their interactive Admin alternatives.
`YAGO_SEARCH_API_KEY` is a static bootstrap credential; the Security page
provides durable scoped search-key creation and revocation instead of persisting
that environment secret. The catalog explicitly classifies these three
bootstrap credentials and proves the corresponding setup or scoped-key surface
exists. The controlled-network secret uses its own write-only persisted setting.

The finite `YACY_*` compatibility aliases and `YAGO_WEB_FALLBACK_ENABLED`,
`YAGO_WEB_FALLBACK_TRIGGER`, and `YAGO_WEB_FALLBACK_PROVIDER` are noncanonical
migration inputs outside the allowlist. Canonical deployment examples do not
expose them.

## Deployment coverage

The root `.env.example` enumerates the union of canonical node and crawler
runtime inputs. The Compose service environments and both systemd environment
examples enumerate exactly the inputs each process reads. Their bootstrap
values agree except for target-specific paths and listener endpoints. Canonical
web fallback configuration uses `YAGO_WEB_FALLBACK_PRIVACY=disabled`; legacy
provider and trigger inputs remain accepted only for upgrade migration.

## Review status

CFG-02 has no known behavior-control parity gap. Every controlling environment
variable is represented by a persisted generic setting or by a bounded dedicated
Admin surface. Environment-only inputs are limited exactly to the durable data
directory, peer identity, crawler identity prefix, and crawler bootstrap
transport named above. Credential bootstraps and migration inputs are separately
classified and are not exceptions. The noncached regressions
`TestRuntimeEnvironmentControlsAreClassified`,
`TestCanonicalEnvironmentOnlyExceptionsRemainExact`,
`TestSettingBackedEnvironmentControlsMatchAdminDefaults`, and
`TestDeploymentExamplesCoverRuntimeEnvironmentControls` prove this inventory,
one-to-one setting mapping, default alignment, and deployment coverage. The AST
inventory treats environment-looking literals and const or var bindings as
controls without depending on a callback name or an `os` import alias; there is
no generic diagnostic exception for an unclassified runtime input.
