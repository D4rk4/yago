# Configuration parity review (CFG-02)

Rule: every environment variable that controls behavior has a runtime admin
setting under Admin → Settings; the env value seeds the default, the stored
setting wins after a restart. This review (2026-07) walked all 67 `YAGO_*`
variables and closed the gaps.

## Covered by admin settings

Search, swarm seeding, web fallback (privacy, backend, seed profile, and — new
in this review — max results, timeout, cache TTL, safe search), remote-search
timeouts, LAN discovery, peer name, advertise host, seedlists, metrics, query
log, ingest quality gate, extract fetch (enabled, and new: timeout, size cap),
new-tab links, snippet fetch, remote-result indexing, portal toggle, HTTPS
redirect, public base URL, autocrawler profile — plus the groups this review
added:

- `storage.quota` — the disk budget (the 1 GB default is trial-only).
- `search.api.require_key` — scoped-key enforcement for the agent API.
- `network.peer.https_preferred`, `network.announce.interval`,
  `network.announce.greets_per_cycle` — swarm presence.
- `dht.*` — participation, distribution, busy-gates, interval, redundancy,
  minimum peer age/connected peers/indexed words.
- `security.egress.allow_private`, `security.egress.allow_cidrs`,
  `security.trusted_proxies`, `security.cors.admin`, `security.cors.search`
  — perimeter knobs (behavior, not secrets; guards rebuild on restart).

## Deliberately env-only

- **Identity and boot facts:** `YAGO_PEER_HASH`, `YAGO_NETWORK_NAME`,
  `YAGO_DATA_DIR`, `YAGO_PEER_BIRTH_DATE` — changing these at runtime forges a
  different node, not a setting.
- **Listener addresses:** `YAGO_PEER_ADDR`, `YAGO_OPS_ADDR`,
  `YAGO_PUBLIC_ADDR`, `YAGO_CRAWL_RPC_ADDR`, `YAGO_ADVERTISE_PORT` — managed
  by the dedicated Listen-addresses admin UI.
- **Secrets:** `YAGO_ADMIN_USER`/`_PASSWORD`, `YAGO_SEARCH_API_KEY` — the
  Security section manages credentials through its own guarded flows, never
  the generic settings catalog.
- **Diagnostics:** `YAGO_PUBLIC_SELF_TEST_URL`.
- `YAGO_WEB_FALLBACK_PROVIDER` names the only implemented provider (ddgs) and
  `YAGO_WEB_FALLBACK_ENABLED` is subsumed by `web.fallback.privacy`
  (`disabled` is the off switch); neither earns a duplicate setting.

## DHT partition exponent

`YAGO_DHT_PARTITION_EXPONENT` shapes how the key space is partitioned and must
match what the node advertised to the swarm since birth; changing it live
would misroute transfers, so it stays env-only on purpose.
