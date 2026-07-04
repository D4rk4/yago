# PLAN.md - Evolve `yago` into a YaCy-compatible Go search peer

Prepared: 2026-07-01

Goal: evolve `yago` from a lightweight Go YaCy RWI/DHT node into a practical
self-hosted YaCy-compatible search peer with P2P participation, crawler
integration, local and federated full-text search, a Tavily-compatible Search
API, and an IBM Carbon administration UI.

This file is the project development roadmap. Work should move in small patch-sized steps, avoid whole-project rewrites, and keep `make verify` green.

---

## 0. Source material

Before changing a planned area, review the relevant files and documents:

1. Upstream fork base:
   - https://github.com/nikitakarpei/yacy-rwi-node
   - https://raw.githubusercontent.com/nikitakarpei/yacy-rwi-node/main/AGENTS.md
   - https://raw.githubusercontent.com/nikitakarpei/yacy-rwi-node/main/Makefile
   - https://raw.githubusercontent.com/nikitakarpei/yacy-rwi-node/main/yacynode/doc/specification.md
   - https://raw.githubusercontent.com/nikitakarpei/yacy-rwi-node/main/yacynode/doc/yacy-dht-interop.md
   - https://raw.githubusercontent.com/nikitakarpei/yacy-rwi-node/main/yacynode/doc/yacy-wire-protocol.md
   - https://raw.githubusercontent.com/nikitakarpei/yacy-rwi-node/main/yacycrawler/README.md
2. YaCy original behavior/API references:
   - https://yacy.net/api/crawler/
   - https://wiki.yacy.net/index.php/Dev%3AAPI
   - https://wiki.yacy.net/index.php/Dev%3AAPIyacysearch
   - https://yacy.net/demonstration_tutorial_screenshot/
   - https://yacy.net/operation/rwi-index-distribution/
3. Tavily compatibility target:
   - https://docs.tavily.com/documentation/api-reference/introduction
   - https://docs.tavily.com/documentation/api-reference/endpoint/search
   - https://docs.tavily.com/documentation/api-reference/endpoint/extract
4. IBM Carbon UI target:
   - https://carbondesignsystem.com/
   - https://carbondesignsystem.com/developing/react-tutorial/overview/
   - https://github.com/carbon-design-system/carbon

Important baseline facts:

- Upstream `yacy-rwi-node` is intentionally a lightweight Go senior YaCy node for DHT/RWI storage and serving, not a full YaCy port.
- Current upstream spec explicitly lists these non-goals: built-in web crawling, built-in HTML parsing/fetching, full-text indexing, local search UI, Solr/Lucene/Elastic integration, outbound DHT distribution, full-scale Go YaCy node.
- Current `yacycrawler` is marked experimental and not production-ready. It can fetch URLs, build YaCy-compatible RWI postings and URL metadata, and publish them toward the node. Verify current node-side order producer and ingest consumer status from code before repeating older documentation claims.
- The fork intentionally reverses part of those non-goals: crawler, search UI, local search, outbound DHT distribution and admin UI become product goals of this fork.
- Do not copy Java YaCy source code into this repository. Reimplement behavior from public protocols, API docs, observed wire fixtures and interoperability tests.
- `yago` is not a literal port of Java YaCy, Solr, Lucene, or Kelondro internals.
- RWI is a compatibility and exchange layer, not the primary local search engine.
- Java, Solr, Lucene, Kelondro, and servlet-style YaCy UI must not become required internal runtime dependencies.

---

## 0.5 Strategic course

The architectural target is a modern Go search appliance that preserves YaCy
wire protocol compatibility and observable peer behavior where implemented.
YaCy compatibility remains a stable external contract, but the local search,
crawler, storage, UI, and future native P2P layers should be designed as modern
bounded components.

Core principles:

1. Preserve YaCy `/yacy/*` wire protocol shapes, network-unit behavior, DHT
   handoff semantics, seedlists, peer liveness, and observable compatibility
   behavior where implemented.
2. Keep RWI generation, storage, and exchange for YaCy P2P compatibility.
3. Do not use RWI as the only or primary local full-text search engine.
4. Add a document store and full-text backend abstraction for local search.
5. Use an embedded pure-Go Bleve full-text backend, tuned for web search.
7. Build Tavily-compatible and YaCy-compatible public search APIs over `yago`'s
   own search core, not as mandatory upstream proxies.
8. Keep remote crawl disabled by default until trust, destination allowlists,
   quotas, and SSRF protections are enforced.
9. Keep native `yago-v2` P2P optional and experimental; it must not break legacy
   YaCy P2P compatibility.

Target runtime architecture:

```text
yago-node
  - YaCy /yacy/* compatibility
  - peer roster, seedlists, liveness, DHT inbound/outbound
  - RWI vault + URL metadata vault
  - P2P policy, quotas, metrics

yago-searchd
  - local full-text index
  - search backend: embedded Bleve (pure Go), tuned for web search
  - document store
  - snippets, phrase/proximity, filters, facets
  - Tavily-compatible POST /search
  - YaCy-compatible /yacysearch.json and /yacysearch.rss adapter

yago-crawld
  - persistent crawl frontier
  - HTTP fast fetch path
  - optional browser slow fetch path
  - robots.txt, sitemap, canonicalization, politeness
  - content extraction and deduplication
  - emits DocumentIngest + RWI postings + URL metadata

yago-admin-ui
  - React/Next.js or Vite React
  - IBM Carbon UI framework
  - admin functionality comparable to original YaCy categories
```

Agent workstreams:

- Architecture Agent: boundaries between node, searchd, crawld, and admin UI;
  internal interfaces; dependency policy; migration from current RWI node to
  search appliance.
- P2P Compatibility Agent: `/yacy/hello.html`, seedlists, peer liveness,
  `/yacy/query.html`, `/yacy/transferRWI.html`, `/yacy/transferURL.html`,
  `/yacy/search.html`, `/yacy/urls.xml`, inbound/outbound DHT, batching, retry,
  peer selection, peer reputation, rate limits, and Java YaCy compatibility
  matrix.
- Native P2P v2 Agent: optional go-libp2p, Kademlia/provider records, signed
  peer metadata, discovery, result federation, and events without changing
  `/yacy/*`.
- Search Core Agent: full-text backend abstraction, document store integration,
  the embedded pure-Go Bleve backend tuned for web search, BM25, field boosts,
  phrase/proximity, snippets, language analyzers, filters, facets, freshness, and
  domain quality.
  Target search backend seam:

  ```go
  type SearchIndex interface {
      Index(ctx context.Context, doc Document) error
      Delete(ctx context.Context, docID string) error
      Search(ctx context.Context, req SearchRequest) (SearchResultSet, error)
      Stats(ctx context.Context) (IndexStats, error)
  }
  ```
- Document Store Agent: canonical URL, normalized URL, title, headings,
  extracted text, optional raw HTML/WARC reference, language, content type,
  fetch status, fetch timestamps, content hash, outlinks, inlinks/anchor text,
  and snippet/Tavily raw-content metadata.
- Tavily API Agent: Tavily-compatible `POST /search` over local full-text,
  optional local semantic/vector search, yago/yacy peers, and an optional
  admin-toggled DDGS web-search fallback only when explicitly enabled.
- YaCy Search API Agent: `/yacysearch.json`, `/yacysearch.rss`, OpenSearch-style
  compatibility, `resource=local|global`, query translation, filters, and
  compatibility notes for unsupported YaCy-specific ranking/profile fields.
- Crawler Agent: persistent frontier, states, robots, politeness, sitemap,
  canonicalization, deduplication, HTTP fast fetch, browser fallback, extraction,
  limits, retry, recrawl scheduling, backpressure, and the node's gRPC crawl transport if used.
- Security Agent: SSRF protections, private/local network denial, DNS rebinding
  protection, crawl sandboxing, size/redirect/scheme limits, API/admin auth,
  remote crawl default-deny, peer quotas, and spam/index poisoning protections.
- Admin UI Agent: IBM Carbon React UI with typed admin APIs, covering search,
  crawl, P2P network, DHT, index/storage, document browser, search backend,
  Tavily API settings, security, remote crawl policy, metrics, logs, and
  configuration.
- Observability Agent: `/health`, `/ready`, Prometheus metrics, structured logs,
  tracing where practical, crawl/fetch/index/search/P2P/storage metrics.
- QA / Compatibility Agent: unit, integration, local multi-peer e2e, race,
  golden YaCy wire, Java YaCy matrix, crawler safety, search relevance, Tavily
  contract, and UI smoke tests.

Roadmap priorities:

- P0 - turn the RWI node into a search engine: align documentation status across
  README, FEATURES, crawler README, and plan files; add document store; add
  search backend abstraction; use an embedded pure-Go Bleve full-text backend
  tuned for web search; implement local Tavily
  `/search`; implement `/yacysearch.json` and `/yacysearch.rss` over search
  core; make snippets come from document store; keep RWI generation and YaCy P2P
  compatibility working.
- P1 - production crawler: persistent frontier, robots/politeness, sitemap
  ingestion, canonicalization, deduplication, browser fallback, backpressure,
  SSRF protection, and recrawl scheduler.
- P1 - P2P/DHT: outbound DHT scheduler, peer selection, batching, retry policy,
  redundancy, delete-after-success policy where safe, peer reputation, strict
  remote crawl policy, and compatibility status endpoint.
- P1 - Carbon admin UI: Search console, Crawl management, P2P network,
  Index/storage, Security, Metrics, Logs/events, and Settings.
- P2 - modern relevance and federation: positional phrase/proximity search,
  domain quality/freshness ranking, optional Qdrant vector/semantic rerank,
  optional local answer generation from snippets, federated result fusion across
  peers, and native libp2p-based `yago-v2` network.

Guardrails:

- Do not reimplement Java YaCy internals blindly.
- Do not introduce JVM, Solr, Lucene, or Kelondro as required runtime
  dependencies.
- Do not query any external web-search provider by default; the DDGS fallback is
  admin-opt-in only.
- Do not use RWI as the only local search index.
- Do not store raw crawled content without size limits and security policy.
- Do not allow remote crawl by default.
- Do not crawl private or local networks.
- Do not let UI drive unstable internal APIs directly; expose typed admin APIs.
- Do not break existing YaCy wire compatibility endpoints.
- Do not delete existing compatibility docs; update them with status notes.

Acceptance criteria for the modern roadmap:

1. Documentation consistently states that `yago` is a modern Go search appliance
   with YaCy wire compatibility, not a Java YaCy/Solr/Kelondro port.
2. RWI remains implemented and tested as compatibility and exchange format.
3. Local search uses a document store and full-text backend abstraction.
4. Crawler output updates document store, RWI postings, and URL metadata.
5. Tavily-compatible `/search` works locally with no external search provider.
6. `/yacysearch.json` and `/yacysearch.rss` adapt the same search core.
7. YaCy `/yacy/*` peer compatibility remains covered by parser, encoder,
   endpoint, integration, and compatibility matrix tests.
8. Remote crawl remains default-deny.
9. Admin UI uses Carbon and stable typed admin APIs.
10. Observability covers health, readiness, search, crawl, storage, and P2P.

---

## 1. Repository operating rules

These rules are mandatory for project work.

### 1.1 Repository rules

1. Read `AGENTS.md` first. Follow its style constraints.
2. Use OCP-style changes: add features behind narrow seams; avoid growing large existing files.
3. Do not introduce generic buckets named `utils`, `helpers`, `manager`, `service`, `handler`, or `types`.
4. Do not add code comments unless explicitly approved. Godoc package docs are allowed if the package needs exported documentation.
5. Each new third-party dependency requires one ADR before use.
6. Pin all runtime and build dependencies.
7. `make verify` is the repository gate. A change is not done until it passes.
8. Maintain or raise the configured coverage threshold.
9. Do not log secrets, API keys, auth headers, cookies or raw request bodies.
10. Keep protocol vocabulary at the transport edge. Inner packages should use domain nouns: peer, seed, posting, reference, document, crawl, query, result, profile.

### 1.2 Task workflow

For each task:

1. Inspect the existing package boundaries.
2. Write/adjust tests first when practical.
3. Implement the smallest vertical slice.
4. Run the narrow test target.
5. Run `make verify` before marking complete.
6. Update docs in `yacynode/doc/` or a new `doc/` location when behavior changes.
7. If adding a dependency, add `yacynode/doc/adr/NNNN-*.md` or a module-local ADR before code.
8. Do not bundle unrelated tasks.

Suggested command pattern:

```sh
make fmt-check
make vet
make lint
make test
make cover-check
make build
make verify
```

After frontend is added, extend `make verify` so it also runs frontend lint, typecheck, unit tests and production build.

### 1.3 Definition of done for the whole fork

The fork is minimally acceptable when all are true:

1. A node can join YaCy-compatible P2P network mode as a reachable senior peer.
2. The node can receive RWI/URL metadata from YaCy-compatible peers.
3. The node can distribute stored RWI/URL metadata to compatible peers when configured.
4. The node can answer local and P2P federated search queries.
5. The node can run crawler jobs, ingest crawler batches and update local search/RWI state.
6. `/yacysearch.json`, `/yacysearch.rss`, `/opensearchdescription.xml` and `/suggest.json` exist with a useful compatibility subset.
7. A Tavily-compatible `POST /search` endpoint exists and works against local/P2P results without requiring any external provider key.
8. The optional DDGS web-search fallback is admin-opt-in, off by default, and sends no query externally until enabled.
9. A Carbon-based admin UI supports search, crawler, network, index, performance, configuration and security pages.
10. `make verify` and e2e tests pass on a clean checkout.
11. Documentation explains configuration, security model, storage model, and compatibility gaps.

---

## 2. Target product shape

Working name: `yago`. Do not rename Go modules or binaries without an ADR and
explicit migration plan.

### 2.1 Product modes

Support three YaCy-like modes:

1. **P2P peer mode**
   - Join YaCy-compatible network.
   - Receive and distribute RWI/URL metadata.
   - Search local and remote peers.
   - Public `/yacy/*` P2P endpoints exposed.
2. **Search portal mode**
   - Local crawler and local search are primary.
   - P2P can be disabled.
   - Public search UI/API can be enabled separately from admin UI.
3. **Intranet mode**
   - Crawl internal sites only.
   - P2P disabled by default.
   - Strong allowlists, no public peer advertisement.

### 2.2 Major backend components

Keep the existing modules unless an ADR approves splitting/renaming:

- `yacymodel`: YaCy value types and codecs.
- `yacyproto`: `/yacy/*` wire protocol DTOs, endpoint names and auth translation.
- `yacynode`: current node daemon and future `yago-node` host for YaCy
  compatibility, peer roster, seedlists, liveness, DHT inbound/outbound, RWI
  vault, URL metadata vault, P2P policy, quotas, and metrics.
- `yacycrawler`: current crawler worker and future `yago-crawld` implementation
  for persistent frontier, fetch, robots, sitemap, canonicalization, extraction,
  deduplication, and `DocumentIngest`/RWI/URL metadata emission.
- `yacycrawlcontract`: gRPC/protobuf message contract between node and crawler,
  to be extended for document ingest without coupling node and crawler packages.

Target components may become separate binaries after ADRs:

- `yago-searchd`: document store, local full-text index on the embedded Bleve
  backend tuned for web search, snippets, phrase/proximity, filters,
  facets, Tavily-compatible `/search`, and YaCy search adapters.
- `yago-admin-ui`: Carbon React admin UI served through stable typed admin APIs.

Add package boundaries inside modules, not catch-all modules:

- `yacynode/internal/peerregistry` - discovered peers, liveness, seedlist import/export, active/known/quarantined state.
- `yacynode/internal/dhtexchange` - inbound/outbound RWI and URL transfer orchestration.
- `yacynode/internal/searchcore` - query parsing, result model, ranking, result merging.
- `yacynode/internal/searchindex` - full-text backend abstraction and backend adapters.
- `yacynode/internal/documentstore` - canonical URL, text, metadata, link, snippet, and raw-content references.
- `yacynode/internal/searchlocal` - local full-text lookup adapter with RWI compatibility support.
- `yacynode/internal/searchremote` - YaCy remote search fanout and response normalization.
- `yacynode/internal/tavilyapi` - Tavily-compatible request/response contract and adapter.
- `yacynode/internal/adminapi` - authenticated JSON API for Carbon UI.
- `yacynode/internal/crawlorders` - node-side crawl order producer.
- `yacynode/internal/crawlingest` - node-side crawler ingest consumer.
- `yacynode/internal/security` - sessions, API keys, password hashing, CSRF, rate limits.
- `yacynode/internal/observability` - metrics, health, queue stats, log event constants.
- `yacynode/internal/searchui` or `yacynode/web` - compiled frontend assets only; source frontend lives in `web/admin`.

### 2.3 Frontend architecture

Use a static React SPA served by the Go node.

Preferred stack:

- TypeScript.
- Vite.
- React.
- `@carbon/react`.
- `@carbon/icons-react`.
- `@carbon/styles`.
- Vitest for unit tests.
- Playwright for UI e2e after the API stabilizes.

Rationale: the node is an appliance-like Go server. A Vite static SPA is easier to embed and serve than SSR. If Next.js is chosen because Carbon's tutorial demonstrates it, add an ADR explaining SSR/build/runtime tradeoffs.

There are two distinct frontends, both Carbon-based but with different runtime
budgets. The authenticated **admin UI** (UI-02..UI-10) is the Carbon React SPA
above, targeting evergreen browsers. The optional **public search portal** (UI-11)
is a separate, admin-toggleable surface on the public port, styled after
early-2000s Yandex and progressively enhanced so it renders and searches in old
browsers and on mobile even with JavaScript disabled — it uses Carbon design
tokens and server-rendered HTML rather than depending on the full `@carbon/react`
runtime. ADR-0020 records that split and the legacy-browser strategy.

---

## 3. Compatibility matrix

### 3.1 YaCy P2P endpoints

Implement and test these in stages:

| Endpoint | Priority | Purpose | Notes |
|---|---:|---|---|
| `/yacy/hello.html` | P0 | Peer ping/liveness | Must return `yourip`, `yourtype`, `seed0`; must perform identity checks. |
| `/yacy/query.html?object=rwicount` | P0 | Capacity/status probe | Needed for senior/principal promotion behavior. |
| `/yacy/transferRWI.html` | P0 | Receive RWI postings | Enforce `youare`; durable write before ack; report `unknownURL`. |
| `/yacy/transferURL.html` | P0 | Receive URL metadata rows | Reconcile URL metadata needed by postings. |
| `/yacy/search.html` | P0 | Remote RWI search | Return `joincount`, `count`, `resourceN`. |
| `/yacy/seedlist.html` | P1 | Peer bootstrap | Plain YaCy seedlist. |
| `/yacy/seedlist.xml` | P1 | Peer bootstrap | XML shape for tools/UI compatibility. |
| `/yacy/seedlist.json` | P1 | Peer bootstrap | JSON shape for tools/UI compatibility. |
| `/yacy/profile.html` | P2 | Peer profile | Minimal public profile. |
| `/yacy/message.html` | P3 | Peer messages | Partial: permission checks and post storage are implemented; optional `iam` is not required, post-only body fields are ignored during permission checks, and attachments remain unsupported. |
| `/yacy/list.html` | P3 | Shared blacklists | Optional, but useful after blacklist support. |
| `/yacy/urls.xml` | P3 | Remote crawl URL lists | Implement only after safe remote crawl policy exists. |
| `/yacy/crawlReceipt.html` | P3 | Remote crawl receipt | Partial: network-auth failures produce no delay field; same-network malformed or wrong target hashes get YaCy retry delay while remote crawl execution is disabled. |

### 3.2 User/search endpoints

| Endpoint | Priority | Purpose | Notes |
|---|---:|---|---|
| `/` | P1 | Public search home | Admin-toggleable Yandex-style public search portal on the public port (UI-11); Carbon-based, progressively enhanced for legacy browsers and mobile. Off by default. |
| `/yacysearch.html` | P1 | YaCy-like result page | Public HTML page. |
| `/yacysearch.json` | P0 | YaCy JSON compatibility subset | Must support `query`, `resource`, `maximumRecords`, `startRecord`, filters. |
| `/yacysearch.rss` | P1 | OpenSearch/RSS compatibility subset | Enough for RSS/OpenSearch clients. |
| `/opensearchdescription.xml` | P1 | Browser search integration | Must reflect public base URL. |
| `/suggest.json` | P2 | Suggestions | From query logs/local index. |
| `/suggest.xml` | P3 | OpenSearch suggestions | Implemented from the current recent-query suggestion source. |
| `/solr/select` | dropped | Solr compatibility subset | Dropped. The native Go full-text search backend replaces Solr query compatibility; not mounted. |
| `/gsa/search` or `/gsa/searchresult` | P3 | GSA compatibility subset | Optional. |
| `POST /search` | P0 | Tavily-compatible Search API | Primary agent/RAG integration target. |
| `POST /extract` | P2 | Tavily-compatible Extract API subset | Optional after content cache/extractor exists. |

### 3.3 Admin endpoints

All admin endpoints require auth unless explicitly marked public.

Base: `/api/admin/v1`.

| Area | Endpoints |
|---|---|
| Auth | `/auth/login`, `/auth/logout`, `/auth/session`, `/auth/password`, `/api-keys` |
| Overview | `/overview`, `/health`, `/ready`, `/metrics/summary`, `/events` |
| Search | `/search`, `/search/explain`, `/search/settings`, `/search/ranking` |
| Tavily | `/tavily/settings`, `/tavily/probe`, `/tavily/usage` |
| Crawl | `/crawl/profiles`, `/crawl/jobs`, `/crawl/jobs/{id}`, `/crawl/jobs/{id}/pause`, `/crawl/jobs/{id}/resume`, `/crawl/jobs/{id}/cancel`, `/crawl/results`, `/crawl/frontier` |
| Network | `/network/peers`, `/network/seedlists`, `/network/self-test`, `/network/dht/transfers`, `/network/dht/gates` |
| Index | `/index/stats`, `/index/documents`, `/index/documents/delete`, `/index/terms`, `/index/storage`, `/index/blacklists` |
| Configuration | `/settings`, `/settings/public-endpoint`, `/settings/mode`, `/settings/storage`, `/settings/proxy` |
| Operations | `/logs`, `/queues`, `/backup`, `/restore`, `/shutdown`, `/restart` |

---

## 4. Data model additions

Do not force a single database decision in the first patch. Use narrow interfaces so storage engines remain replaceable, matching upstream constraints.

### 4.1 Peer data

Add persistent records for:

- Peer hash.
- Peer name.
- Peer type: virgin, junior, senior, principal, disconnected, unknown.
- Public host and port.
- Alternative addresses discovered from liveness.
- Seed flags.
- Software version, network name, last seed payload.
- Last seen time.
- Last successful hello time.
- Last successful query time.
- RWI count/capacity reported by peer.
- Latency and failure streak.
- State: active, known, quarantined, blocked.
- Last DHT send/receive statistics.

### 4.2 Search/index data

Keep existing RWI/URL metadata storage. Add fields or side stores for:

- URL hash.
- Canonical URL.
- Title.
- Host.
- Path.
- MIME/content domain.
- Language.
- Last modified date.
- Crawled date.
- Content size.
- Word count.
- Outbound link count.
- Inbound reference count if available.
- Excerpt/snippet text if configured.
- Optional raw content cache if configured.
- Content hash for dedupe.
- Ranking signals.

Default privacy/storage mode:

- Do not store full document bodies.
- Store title, metadata and short excerpt/snippet only when enabled.
- Raw content cache is disabled by default and must have quota/TTL controls.

### 4.3 Crawler data

Add persistent records for:

- Crawl profile.
- Crawl job.
- Crawl frontier item.
- Crawl fetch attempt.
- Crawl result.
- Host politeness state.
- Robots.txt cache.
- Sitemap state.
- Failed URL state.
- Ingest batch offset/checkpoint.

### 4.4 Security data

Add persistent records for:

- Admin users.
- Password hash parameters.
- Active sessions.
- API keys, stored only as hashes.
- API key scopes.
- Login failure counters.
- CSRF tokens or same-site session strategy.

---

## 5. Phase 0 - Fork preparation and repository audit

Goal: establish a safe working base without feature changes.

### FND-01: Create fork metadata and docs

Tasks:

1. Add `FORK.md` describing fork goals and compatibility claims.
2. Add `yacynode/doc/fork-roadmap.md` summarizing this plan in user-facing language.
3. Add `yacynode/doc/compatibility.md` with explicit tables for supported, partial and unsupported YaCy/Tavily endpoints.
4. Preserve AGPL notices and add UI legal notice requirements.

Acceptance:

- `make verify` passes.
- Docs do not claim full YaCy compatibility.
- Docs state that `POST /search` is Tavily-compatible API surface, not Tavily itself, unless external upstream mode is enabled.

### FND-02: Add ADR process for new dependencies

Tasks:

1. Create `yacynode/doc/adr/0000-template.md`.
2. Document dependency ADR rules from `AGENTS.md`.
3. Add an ADR index.

Acceptance:

- No code dependency added yet.
- `make verify` passes.

### FND-03: Baseline e2e fixture capture

Tasks:

1. Add test fixtures for YaCy wire responses already supported by upstream.
2. Add golden fixtures for `hello.html`, `query.html`, `transferRWI.html`, `transferURL.html`, `search.html`.
3. Add a fixture naming convention under `yacynode/test/fixtures/yacywire/`.

Acceptance:

- Tests compare parse/encode round trips.
- No network dependency in unit tests.

---

## 6. Phase 1 - P2P hardening and DHT completion

Goal: make the node a reliable YaCy-compatible peer, first inbound, then outbound.

### P2P-01: Peer registry and seedlist ingestion

Tasks:

1. Implement `peerregistry` with persistent known peers and in-memory active view.
2. Import configured seedlists on schedule.
3. Randomize peer selection for liveness responses while honoring requested count.
4. Do not redistribute peers only self-reported in inbound requests unless they were also confirmed through configured seedlists or successful active probes.
5. Add admin-visible peer states: active, known, quarantined, blocked.
6. Add metrics:
   - `yacy_peer_known_total`
   - `yacy_peer_active_total`
   - `yacy_peer_probe_failures_total`
   - `yacy_seedlist_imports_total`

Acceptance:

- Unit tests cover seed decode, duplicate peer merge, random selection bound, quarantine transition.
- Metrics tests cover labels and counters.
- Admin API can list peers after this task only if `adminapi` exists; otherwise expose internal test seam.

### P2P-02: Public endpoint self-test

Tasks:

1. Add a self-test that checks advertised public host/port.
2. Validate that `/yacy/query.html?object=rwicount` is reachable from the configured public URL when external probe is enabled.
3. Provide a local-only fallback check when no external probe URL is configured.
4. Surface results in logs, metrics and later UI.

Acceptance:

- No external call is made unless explicitly configured.
- Failure does not crash the node; it changes readiness/gate state.
- Tests cover success, timeout, wrong peer hash, wrong response shape.

### P2P-03: Inbound RWI durability and reconciliation

Tasks:

1. Audit existing `transferRWI.html` handling.
2. Ensure durable write before success response.
3. Enforce request body limits, batch limits and deadlines.
4. Implement `unknownURL` reporting for URL metadata that is missing.
5. Add backpressure response when quota/disk/queue is exhausted.
6. Add replay/deduplication behavior based on word hash + URL hash + peer + sequence if available.
7. Add metrics:
   - `yacy_rwi_received_postings_total`
   - `yacy_rwi_rejected_postings_total`
   - `yacy_rwi_unknown_url_total`
   - `yacy_rwi_ingest_duration_seconds`

Acceptance:

- Tests simulate partial batch failure and disk quota failure.
- Acknowledged postings survive restart in a storage-backed test.
- Unknown URL response shape matches YaCy-compatible fixture.

### P2P-04: URL metadata ingest

Tasks:

1. Audit existing `transferURL.html` handling.
2. Validate URL metadata rows at transport edge.
3. Normalize URLs consistently.
4. Reconcile pending unknown URL hashes from prior `transferRWI.html` calls.
5. Update search metadata side store.
6. Add metrics:
   - `yacy_url_metadata_received_total`
   - `yacy_url_metadata_rejected_total`
   - `yacy_url_metadata_reconciled_total`

Acceptance:

- Tests cover indexed `url0..urlN` rows.
- Invalid URL rows do not poison the batch.
- Missing metadata reconciliation unblocks local search results.

### P2P-05: Remote search responder completeness

Tasks:

1. Audit existing `/yacy/search.html` handling.
2. Implement query-to-RWI lookup against local index.
3. Return `joincount`, `count`, and `resourceN` rows.
4. Apply deadline and result count limits.
5. Do not perform outbound network fetch in remote search responder.
6. Add cache-friendly deterministic sorting.

Acceptance:

- Golden tests for response format.
- Query with no hits returns valid empty response.
- Multi-term query returns intersection/merge behavior with deterministic ranking.

### P2P-06: Outbound DHT distributor

Tasks:

1. Add `dhtexchange` outbound queue.
2. Select target peers based on YaCy word hash ring position and active peer capability.
3. Probe peer `query.html?object=rwicount` before distribution when capacity is stale.
4. Batch `transferRWI.html` calls with strict body/deadline limits.
5. Send missing URL metadata with `transferURL.html` when peer reports `unknownURL`.
6. Retry transient failures with exponential backoff and jitter.
7. Quarantine peers after repeated protocol or transport failures.
8. Provide config flags equivalent in spirit to:
   - `network.unit.dht`
   - `allowDistributeIndex`
   - `allowDistributeIndexWhileCrawling`
   - `allowDistributeIndexWhileIndexing`
9. Add metrics:
   - `yacy_dht_outbound_batches_total`
   - `yacy_dht_outbound_postings_total`
   - `yacy_dht_outbound_failures_total`
   - `yacy_dht_outbound_unknown_url_total`

Acceptance:

- Unit tests for target selection.
- Integration tests with two local Go nodes transfer postings end-to-end.
- E2E fixture test with a Java YaCy peer is added behind `-tags e2e` if a pinned Docker image is available.
- Distributor never runs when disabled.

### P2P-07: Sender-side DHT gates dashboard data

Tasks:

1. Represent each DHT gate as a named boolean with reason text.
2. Gates include public reachability, active peer count threshold, network enabled, distribution enabled, local RWI minimum, crawl/index queue conditions and storage health.
3. Expose gates via admin API for UI.
4. Log only changes, not every evaluation tick.

Acceptance:

- Tests cover every gate reason.
- UI can later render gate status without parsing logs.

### P2P-08: Optional remote crawl compatibility policy

Do not implement remote crawl blindly. Treat it as a security-sensitive optional phase.

Tasks:

1. Add `yacynode/doc/remote-crawl-policy.md`.
2. Define disabled-by-default behavior for `/yacy/urls.xml` and `/yacy/crawlReceipt.html`.
3. Define allowlist rules: trusted peers only, max URLs, allowed schemes, allowed domains/IP ranges, rate limits.
4. Implement 501 or empty safe responses until policy is enabled.

Acceptance:

- Remote peers cannot make the node crawl arbitrary private/internal URLs by default.
- Docs explain SSRF risks and safe defaults.

---

## 7. Phase 2 - Local and federated search

Goal: make search useful locally, across YaCy peers and through compatibility APIs.

### SEARCH-01: Search domain model

Tasks:

1. Add `searchcore` request/result model independent from YaCy and Tavily transport shapes.
2. Model fields:
   - query text
   - parsed terms
   - filters
   - source selection: local, p2p, ddgs-fallback
   - limit/offset
   - sort mode
   - content domain
   - language
   - include raw content flag
   - include answer flag
3. Model result fields:
   - title
   - URL
   - display URL
   - snippet/content
   - score
   - source
   - host
   - path
   - MIME/content domain
   - language
   - dates
   - favicon URL if known
   - images if known
   - explanation when requested

Acceptance:

- No transport JSON/form tags in core domain structs unless using dedicated DTO wrappers.
- Tests cover zero values and validation.

### SEARCH-02: Query parser compatibility subset

Tasks:

1. Support plain terms and quoted phrases.
2. Support YaCy-like modifiers:
   - `/date`
   - `LANGUAGE:en` and `lr=lang_en`
   - `inurl:`
   - `site:`
   - `tld:`
   - `filetype:`
   - `NEAR` as a ranking hint, not necessarily exact positional search in first version.
3. Support request params:
   - `query`
   - `startRecord`
   - `maximumRecords`
   - `resource=local|global`
   - `contentdom=text|image|audio|video|app|all`
   - `urlmaskfilter`
   - `prefermaskfilter`
   - `verify=false|cacheonly|iffresh|ifexist|true`
   - `nav=none|all`
4. Fail closed on invalid regex filters.
5. Cap `maximumRecords` for unauthenticated requests.

Acceptance:

- Golden tests for YaCy query examples.
- Fuzz tests for parser if practical.
- Invalid modifiers return user-readable errors in APIs.

### SEARCH-03: Local RWI lookup

Tasks:

1. Implement local term-to-word-hash lookup.
2. Resolve postings to URL metadata.
3. Intersect multi-term postings.
4. Rank results with deterministic scoring.
5. Deduplicate canonical URLs.
6. Produce snippets from title, metadata and optional excerpts.

Initial ranking signals:

- Term match count.
- Title match boost.
- URL/path match boost.
- Host/path quality.
- Recency when dates exist.
- Inbound reference count if available.
- Content domain match.
- P2P source penalty/boost configurable.

Acceptance:

- Tests cover single-term, multi-term, missing metadata, dedupe, offset/limit.
- No full body storage required.
- Query latency is bounded for large posting lists by configured caps.

### SEARCH-04: Optional embedded full-text index ADR

Status: accepted in `yacynode/doc/adr/0012-use-bleve-for-embedded-full-text-fallback.md`,
amended by `yacynode/doc/adr/0018-commit-to-bleve-web-search-backend.md`.
The local search backend is a persistent Bleve v2 `SearchIndex` stored under
`YACY_DATA_DIR/search.bleve`. It opens the existing index on startup and rebuilds
from the document store only when the index is missing or unusable. Bleve is the
committed web-search backend, tuned for web search (SEARCH-09 through SEARCH-11);
the Tantivy production sidecar is dropped from the roadmap.

Decision considered:

1. Use existing RWI only, no embedded full-text dependency.
2. Add Bleve v2 for embedded full-text in pure Go. Selected for the first fallback.
3. Add SQLite FTS5, noting CGO/runtime tradeoffs.
4. Add external search backend adapter, keeping default RWI-only.

ADR must decide:

- Storage size expectations.
- Raspberry-Pi-class resource usage.
- Query features needed for snippets and phrases.
- Build portability.
- Index rebuild strategy.
- Backup/restore implications.

Acceptance:

- ADR accepted before any dependency lands.
- If dependency is added, versions are pinned and `make verify` uses pinned tooling.

### SEARCH-05: Federated YaCy remote search

Tasks:

1. Implement `searchremote` fanout.
2. Select candidate peers from word hash ring and active peers.
3. Query peers in parallel with bounded concurrency.
4. Enforce per-peer and overall deadline.
5. Normalize `resourceN` rows into `searchcore.Result`.
6. Merge local and remote results.
7. Deduplicate by URL/canonical URL.
8. Track partial failures without failing the whole search.

Acceptance:

- Tests simulate peers with successes, timeouts, malformed responses and duplicates.
- UI/API can show partial result warnings.
- `resource=local` never calls remote peers.
- `resource=global` calls local + remote unless config disables P2P search.

### SEARCH-06: YaCy search API compatibility

Tasks:

1. Implement `/yacysearch.json` using `searchcore`.
2. Implement `/yacysearch.rss` with OpenSearch-compatible channel metadata.
3. Implement `/opensearchdescription.xml`.
4. Implement `/suggest.json` from recent queries and high-frequency terms.
5. Implement `/yacysearch.html` as either Carbon public search page or a simple redirect to the SPA route.

Acceptance:

- Golden JSON and RSS tests.
- Browser OpenSearch description points to the correct public base URL.
- Unauthenticated limit cap is enforced.
- Global search shows partial remote errors in metadata, not as HTTP 500.

### SEARCH-07: Search explain and ranking config

Status: acceptance met. `POST /api/admin/v1/search/explain` previews a query
under caller-supplied ranking weights and returns per-result score explanations
without saving them; `RankingWeights` validates and threads through the memory
and disk Bleve searches with a documented default profile.

Tasks:

1. Add explain output for admin users. Done.
2. Add configurable ranking weights. Done.
3. Validate ranking config before saving. Done (per-request validation).
4. Store ranking config in persistent settings. Deferred: the preview path is
   stateless by design; persisting an applied profile and threading it into the
   live default searcher (with search-cache invalidation on change) is a
   follow-up, not required by the acceptance criteria below.
5. Provide default profile matching YaCy-ish behavior without overclaiming exact
   compatibility. Done (`DefaultRankingWeights`: title 4, headings 3, anchors 2,
   body 1, url 1).

Acceptance:

- Admin API can preview ranking changes without saving. Met.
- Tests cover config validation and deterministic scoring. Met.

### SEARCH-08: Commit to Bleve as the web-search backend

Status: accepted in `yacynode/doc/adr/0018-commit-to-bleve-web-search-backend.md`.

Tasks:

1. Commit to the embedded Bleve backend for local web search; drop the Tantivy production sidecar from the roadmap.
2. Remove the Tantivy migration framing from README, FEATURES, specification, ADR-0012, AGENTS, and this plan.

Acceptance:

- Docs describe Bleve as the committed backend tuned for web search, with RWI exchange-only.
- `make verify` passes; no dependency added.

### SEARCH-09: Tune the Bleve index mapping for web search

Tasks:

1. Replace the default index mapping with a shared custom mapping for the memory and disk indexes.
2. Index only the queried fields, without the `_all` composite field, stored fields, term vectors, or doc values the node does not use.

Acceptance:

- Query semantics are unchanged; existing search behavior tests stay green.
- Index size and per-query work drop; `make verify` passes.

### SEARCH-10: Hot-query cache and index warmup

Tasks:

1. Add a bounded hot-query result cache as a decorator over `SearchIndex`, invalidated on index writes.
2. Warm the disk index on open.

Acceptance:

- Cache never returns stale results after an index write.
- Tests cover cache hit, invalidation, and eviction; `make verify` passes.

### SEARCH-11: Web-search relevance and analyzer tuning

Tasks:

1. Tune analyzers for web content, including URL and host tokenization and language-aware text analysis where language is known.
2. Improve result quality with phrase/proximity support and per-host result diversity.

Acceptance:

- Representative queries improve or hold on a before/after relevance check.
- Tests cover the analyzer and diversity behavior; `make verify` passes.

---

## 8. Phase 3 - Tavily-compatible search

Goal: make the fork usable by agents/RAG tools that expect Tavily's Search API shape.

Important interpretation:

- Implement an inbound Tavily-compatible API surface: `POST /search` with Tavily-like request/response JSON.
- Do not integrate any external commercial search API; there is no outbound upstream Tavily provider. Instead, offer an optional, admin-toggled DDGS web-search fallback that triggers only on a local-plus-federated miss, records a `ddgs` provenance on each fallback result, and can seed the crawler from the discovered URLs so the next identical query is answered locally. It is disabled by default and needs no API key. The `[ddgs]` marker is a presentation concern rendered only on the human search surfaces (the public and admin search UI and the `/yacysearch.*` endpoints they call); the Tavily-compatible `POST /search` API is a drop-in and returns the same fallback results unmarked and Tavily-shaped.
- Do not require a Tavily API key for local/P2P search, and never send the user query to any external provider by default.

### TAVILY-01: Contract DTOs and validation

Status: partial implementation exists in `yacynode/internal/tavilyapi`.
`POST /search` accepts the current field set, ignores unknown fields for forward
compatibility, validates bounded options, returns `request_id`, and uses stable
JSON error envelopes. Opt-in local bearer auth is implemented through
`YAGO_SEARCH_API_KEY`. Stored page image metadata is returned when
`include_images` is requested. Scopes, generated answers, image ranking/search,
real usage accounting, hashed key storage, rate limits, and the optional DDGS
web-search fallback remain separate tasks.

`POST /search` is a **drop-in replacement for the Tavily Search API** and stays
maximally compatible with it: the response carries only Tavily-shaped fields and
never yago-specific provenance markers (no `[ddgs]` tag, no owned-vs-federated
source labels). It is search-only and does not browse or fetch result pages
inline (that is `POST /extract`, TAVILY-05). When the optional DDGS fallback
(TAVILY-04) is enabled and a query misses locally and across peers, this endpoint
returns the fallback results unmarked and shaped like native Tavily results; the
`[ddgs]` marker belongs to the human search surfaces (UI and `/yacysearch.*`),
not here.

Implement `tavilyapi` DTOs for `POST /search`.

Request fields to support:

- `query` required string.
- `search_depth`: `basic`, `advanced`, `fast`, `ultra-fast`.
- `chunks_per_source`: integer 1..3, effective for advanced mode.
- `max_results`: integer 0..20.
- `topic`: `general`, `news`, `finance`.
- `time_range`: `day`, `week`, `month`, `year`, `d`, `w`, `m`, `y`.
- `start_date`: `YYYY-MM-DD`.
- `end_date`: `YYYY-MM-DD`.
- `include_answer`: boolean or `basic`/`advanced` string.
- `include_raw_content`: boolean or `markdown`/`text` string.
- `include_images`: boolean.
- `include_image_descriptions`: boolean.
- `include_favicon`: boolean.
- `include_domains`: string array.
- `exclude_domains`: string array.
- `country`: string enum, pass through as ranking/filter hint.
- `auto_parameters`: boolean.
- `exact_match`: boolean.
- `include_usage`: boolean.
- `safe_search`: boolean; if unsupported, enforce configured local safety/blacklist policy and document limitation.

Response fields:

- `query`.
- `answer` only when `include_answer` requested.
- `images`.
- `results[]`:
  - `title`
  - `url`
  - `content`
  - `score`
  - `raw_content`
  - `favicon`
  - `images`
- `response_time`.
- `auto_parameters` when requested.
- `usage` when requested.
- `request_id`.

Acceptance:

- JSON schema-style validation tests for all fields.
- Unknown fields are ignored or rejected by config; default should ignore for forward compatibility.
- Error responses are stable JSON and include request ID.

### TAVILY-02: Local/P2P adapter

Tasks:

1. Map Tavily request to `searchcore.Query`.
2. `search_depth` controls latency/relevance tradeoff:
   - `ultra-fast`: local only, small timeout, no snippets beyond metadata.
   - `fast`: local first, optional tiny remote fanout.
   - `basic`: local + P2P within normal timeout.
   - `advanced`: local + broader P2P + richer snippets/chunks.
3. `max_results` maps to limit with max 20.
4. `include_domains`/`exclude_domains` map to host filters.
5. Date filters map to metadata date filters.
6. `include_raw_content` returns `null` unless raw content cache is enabled and entry exists.
7. `include_answer` returns an extractive answer from top snippets when configured; otherwise return empty string with metadata warning or omit based on compatibility tests.
8. `include_usage` returns local synthetic usage, not Tavily billing usage.
9. When the DDGS fallback (TAVILY-04) supplies results, strip the internal `ddgs`
   provenance before serialization so the Tavily response stays drop-in: no
   `[ddgs]` marker and no extra source fields. The same fallback results keep
   their `ddgs` provenance when rendered on the human search surfaces.

Acceptance:

- `curl -X POST localhost:<port>/search -H 'Authorization: Bearer <local-api-key>' -d '{"query":"test"}'` works.
- No external network call occurs in local/P2P mode.
- Tests compare response shape to Tavily docs examples without copying example content.
- With the DDGS fallback enabled, a `POST /search` response over fallback results
  is byte-shape-identical to an owned-result response of the same size: no
  provenance markers leak into the Tavily surface.

### TAVILY-03: Auth for Tavily-compatible endpoint

Status: acceptance met. Two auth modes back the Tavily surface. The legacy
static mode (`YAGO_SEARCH_API_KEY`) keeps a single bearer token; the scoped mode
(`YAGO_SEARCH_REQUIRE_API_KEY=true`) routes `POST /search` and `POST /extract`
through the admin auth service's vault-backed, hashed API-key store via a
bearer-only `adminauth.APIKeyAuthorizer` (never a session cookie). The admin
service is now provisioned once and shared, so keys minted on the operations
surface authenticate on the public surface with the same per-key rate limiter and
audit observer. `/search` needs `search:read`, upgraded to `search:raw` when the
request asks for raw content; `/extract` needs `search:raw`. Missing or malformed
credentials return a Tavily-shaped `401` with `WWW-Authenticate: Bearer`,
insufficient scope returns `403`, and a throttled key returns `429`. When neither
mode is configured the surface stays public. API keys are hashed at rest and shown
once at creation; auth headers are never logged. The `search:admin` scope and a
public-mode strict rate limit remain planned; see the notes below.

Tasks:

1. Support local API keys via `Authorization: Bearer`.
2. Add API key scopes:
   - `search:read`
   - `search:raw`
   - `search:admin`
3. Allow optional unauthenticated public mode with strict rate limits.
4. Add per-key rate limits and audit events.
5. Hash stored API keys.

Acceptance:

- Missing/invalid auth returns 401 unless public mode enabled.
- Scope failure returns 403.
- API key value appears only once at creation and never in logs.

### TAVILY-04: Optional DDGS web-search fallback provider

Status: core implemented, env-gated. A `websearch.FallbackSearcher` decorator
wraps the federated searcher (`node_public_search.go`) so both the Tavily API and
the human search surfaces share one fallback: on a true miss (zero results) and
only while enabled, it queries a keyless multi-engine metasearch provider and
stamps results `searchcore.SourceWeb`. The provider (`websearch.DDGSProvider`)
routes through the egress guard, caches responses briefly, backs off on
`202`/`429`, and degrades to empty on failure. Configured via `YAGO_WEB_FALLBACK_*`
(off by default). The runtime admin toggle (flip without restart) and the SEC-05
tri-state privacy mode remain planned; today the enable flag is read from config.

Supersedes the earlier "optional real Tavily upstream provider" idea. There is no
outbound commercial Tavily integration. When the node cannot answer a query from
its own index or its federated peers, an operator may opt in to a DDGS-style
web-search fallback (DDGS-family metasearch) so the caller still gets results,
tagged so they are never confused with owned index hits.

DDGS is a keyless, unofficial metasearch idea (rotate across public engines) with
real rate limits (`202 Ratelimit`) and per-engine terms of service. The Go node
ships its own provider; the concrete backend is fixed in ADR-0021: an in-house,
structure-driven multi-engine client whose default `auto` backend **excludes
DuckDuckGo** (it aggressively blocks automated queries) and uses Mojeek then Bing,
with DuckDuckGo (`html`/`lite`) available only when selected explicitly. It
promotes `golang.org/x/net` to a direct dependency for HTML parsing (recorded in
ADR-0021). Any further third-party dependency needs its own ADR before use.

Tasks:

1. Add a pluggable `WebSearchProvider` port behind a narrow interface; a
   DDGS/DuckDuckGo backend is the first (and only planned) implementation. No API
   key.
2. Add config, defaulting to disabled, plus an admin toggle so operators flip it
   at runtime without a restart:
   - `YAGO_WEB_FALLBACK_ENABLED=false`
   - `YAGO_WEB_FALLBACK_PROVIDER=ddgs`
   - `YAGO_WEB_FALLBACK_BACKEND=auto`
   - `YAGO_WEB_FALLBACK_MAX_RESULTS`
   - `YAGO_WEB_FALLBACK_TIMEOUT`
   - `YAGO_WEB_FALLBACK_SAFESEARCH`
3. Invoke the provider only on a true miss: after both the local search and the
   federated peer/cache search return zero results for the request window. Never
   as a primary source and never mixed silently.
4. Record source `ddgs` on every fallback result as an internal provenance;
   owned local/federated hits keep their existing sources. This provenance is a
   shared mechanism consumed by two surfaces with different presentation rules:
   - The human search surfaces (the public search portal, the admin search UI,
     and the `/yacysearch.*` endpoints they call) render it as a visible
     `[ddgs]` marker so users never confuse external hits with owned index hits.
   - The Tavily-compatible `POST /search` API (TAVILY-01) strips the provenance
     and returns the results unmarked and Tavily-shaped, preserving drop-in
     compatibility. The marker never appears on that surface.
5. Route the outbound query through the in-process egress guard; enforce
   rate-limit backoff and cache provider responses (short TTL) to respect
   DuckDuckGo/DDGS limits and reduce repeat egress.
6. Privacy: gate behind SEC-05 (off / explicit-per-request / enabled); never send
   the user query externally by default; redact query logs and provider errors
   per the active privacy mode.

Acceptance:

- Fallback is disabled by default; with it disabled, `/search` and
  `/yacysearch.json` behavior is unchanged (a miss stays a miss).
- Tests use an httptest fake provider only; no real DuckDuckGo/DDGS call in CI.
- With the fallback enabled, a local-plus-federated miss on the human search
  surfaces (public portal, admin UI, `/yacysearch.*`) returns `[ddgs]`-marked
  results, while the Tavily-compatible `POST /search` returns the same results
  unmarked and Tavily-shaped; the rate-limit/backoff and egress-guard paths are
  covered.

### TAVILY-06: Seed crawl from DDGS-discovered URLs

Status: acceptance met. A `webCrawlSeeder` (`node_web_seed.go`) is installed on the
`websearch.FallbackSearcher` via `WithSeeder` whenever `YAGO_WEB_FALLBACK_SEED_CRAWL`
is on and the crawl runtime is active (`crawlOrderQueue` extracts the durable queue;
nil when crawling is disabled, so there are no side effects). On a fallback miss the
seeder publishes one conservative crawl order per new URL through
`crawldispatch.CrawlOrderQueue.PublishOnce` keyed by URL (idempotent across
restarts), skipping URLs already in the document store and non-HTTP(S) URLs. The
seed profile is `ScopeDomain`, shallow (`YAGO_WEB_FALLBACK_SEED_DEPTH`, default 1)
with a per-host cap (`YAGO_WEB_FALLBACK_SEED_MAX_PAGES`, default 20); robots, egress
deny, and per-host limits are enforced downstream by the crawler. Tests use a fake
queue and fake document directory; no live crawl in CI.

Closes the loop for the DDGS fallback: when the fallback surfaces URLs the node
has never seen, hand them to the crawler so the pages are fetched and indexed and
the next identical query is answered from the local index instead of hitting the
external provider again.

Tasks:

1. When the DDGS fallback returns URLs, publish a crawl order through
   `crawldispatch.CrawlOrderQueue` seeding those URLs.
2. Deduplicate and rate-limit seed submissions: idempotency by normalized URL; do
   not re-seed URLs already in the document store or recently queued.
3. Use a conservative default crawl profile (shallow depth, domain range,
   politeness, robots-respecting, egress-guarded) so search-driven seeding cannot
   amplify into unbounded crawls; make it admin-configurable.
4. Gate behind the DDGS fallback admin toggle, with a separate sub-toggle to allow
   fallback search without auto-seeding.

Acceptance:

- With seeding enabled, a fallback miss enqueues a durable crawl order for the
  discovered URLs; disabled, there are no crawl side effects.
- Seeding respects robots, egress deny, and per-host caps; duplicates are not
  re-seeded.
- Tests use the embedded/fake broker; no live crawl in CI.

### TAVILY-05: Tavily `/extract` compatibility subset

Status: acceptance met. `POST /extract` returns Tavily-shaped results for URLs
already in the document store, and an uncached URL becomes a controlled
`failed_result`. Fetch-on-extract is now available as an opt-in
(`YAGO_EXTRACT_FETCH_ENABLED`, off by default): when on, an uncached URL is
fetched through the shared egress-guarded client (so private networks stay
default-denied — no SSRF), bounded by `YAGO_EXTRACT_FETCH_MAX_BYTES` and
`YAGO_EXTRACT_FETCH_TIMEOUT`, and its title and visible text are extracted with
`golang.org/x/net/html` (a new `internal/extractfetch` package;
`script`/`style`/`noscript` stripped, whitespace collapsed). Non-`200` or
non-HTML responses and any fetch error degrade to a generic `failed_result`
(the internal error is never reflected, avoiding an SSRF oracle). Image
extraction and readability-grade boilerplate removal on fetched pages remain a
future refinement. Off by default, so there is no outbound request or SSRF
surface unless an operator opts in.

Tasks:

1. Implement `POST /extract` for URLs already in cache/index.
2. If URL is not cached and fetch-on-extract is disabled, return controlled error.
3. If fetch-on-extract is enabled, apply same crawler safety policy.
4. Return extracted title, text/markdown, metadata and images if available.

Acceptance:

- Disabled by default unless content cache/extractor is production-ready.
- No SSRF against private networks by default.

---

## 9. Phase 4 - Crawler productionization

Goal: make `yacycrawler` usable as a real crawler worker and wire it to the node.

### CRAWL-01: Node-side order producer

Status: acceptance met. The node's crawl dispatch persists an order in the durable
vault-backed queue before delivery, and a repeated crawl-start request carrying the
same `Idempotency-Key` header enqueues at most one order (`DurableOrderQueue.PublishOnce`
checks and records the key in the same transaction as the enqueue, so a retry does
not create a second order; the response reports `duplicate: true` with `200`). The
YaCy `/Crawler_p.html` intake stays Unsupported by design (CRAWL-08).

Tasks:

1. Implement `crawlorders` in `yacynode`. Done via `crawldispatch` + `crawlbroker`.
2. Accept crawl job requests through admin API. Done (`POST /crawl`); the
   YaCy-compatible `/Crawler_p.html` subset stays Unsupported by design (CRAWL-08).
3. Persist crawl job before enqueuing order to the node's durable crawl queue. Done.
4. Publish orders using `yacycrawlcontract` message types. Done.
5. Include job ID, profile ID, start URLs, depth, range, filters and politeness
   hints. Done through the crawl profile and requests.
6. Add idempotency key for duplicate start requests. Done (`Idempotency-Key`
   header; atomic check-and-record in the durable queue).

Acceptance:

- A crawl job survives node restart before crawler picks it up. Met.
- Duplicate submit with same idempotency key does not create duplicate jobs. Met.
- Tests use embedded/fake broker when possible. Met.

### CRAWL-02: Node-side ingest consumer

Tasks:

1. Implement `crawlingest` in `yacynode`.
2. Consume crawler ingest batches over the node's gRPC crawl endpoint.
3. Validate batch schema and job ownership.
4. Write URL metadata, RWI postings, snippets and crawl result state durably.
5. Ack broker message only after durable commit.
6. Apply backpressure when storage/queue is unhealthy.
7. Update crawl job progress counters.

Acceptance:

- Crash/restart test does not lose acknowledged batches.
- Malformed batches are rejected and recorded.
- Ingested pages are searchable locally.

### CRAWL-03: Crawl profile model

Status: acceptance met. `CrawlProfile.Validate()` in the shared crawl contract
blocks the dangerous defaults called out below - an impossible crawl or index
must-match/must-not-match URL regex, negative or unbounded crawl depth (capped at
`MaxCrawlDepth`), a non-positive pages-per-host cap, and negative recrawl or delay
durations - and the node crawl dispatch rejects such requests with a `400` before
publishing. Private-network destinations are already blocked by the crawler
fetch-safety path (CRAWL-04). Beyond the crawl-admission regexes, the model now
carries index-time `IndexURLMustMatch`/`IndexURLMustNotMatch` rules, compiled in
`crawladmission` (`IndexAllowed`) and consumed in the crawler pipeline: a page
that fails the index rules is still fetched and its links are still followed, but
it is not indexed or emitted. The remaining expert fields (per-host/global
concurrency, cache and delete-old policy, byte caps, media flags) and the UI
profile editor (UI-06) land with their downstream consumers, matching the
existing pattern.

Implement profile fields comparable to YaCy advanced crawler where practical:

- Start mode: `url`, `sitemap`, `sitelist`, `file`.
- Start URLs.
- Depth.
- Depth extension regex.
- Range: `wide`, `domain`, `subpath`.
- Must-match URL regex.
- Must-not-match URL regex.
- IP allow/deny regex or CIDR rules.
- Index must-match URL regex.
- Index must-not-match URL regex.
- Canonical policy.
- Recrawl policy: no doubles, reload if older.
- Delete old policy: off, on, age.
- Cache policy.
- Index text flag.
- Index media flag.
- Exclude stop words flag.
- Remote indexing/distribution flag.
- User agent.
- Max pages.
- Max bytes per document.
- Max total bytes.
- Per-host concurrency.
- Global concurrency.
- Delay per host.
- Robots.txt policy.
- Sitemap discovery policy.

Acceptance:

- Validation blocks dangerous defaults: unbounded depth, private IP crawl in public mode, impossible regex.
- UI can create simple and expert profiles using same backend model.

### CRAWL-04: Fetch safety and politeness

Status: partial implementation exists. The crawler now enforces HTTP(S)-only
public-web target admission before robots.txt and browser fetch, blocks literal
and DNS-resolved non-public destinations, fails closed on DNS resolution errors,
uses a bounded HTTP fast fetch path for ordinary HTML pages, falls back to the
browser only when the fast path rejects the page, enforces an explicit
configurable redirect-hop limit on the HTTP fast path, applies explicit
request, connect, TLS, and response-header timeout budgets, and checks the final
fetched URL against the same public-web policy. Redirect-to-private is already
blocked on both fetch paths at dial time by the in-process egress guard
(ADR-0013), so a public URL that redirects to a private address fails to connect
on the HTTP path and through the browser's guarded forward proxy alike. The MIME
allowlist is now a single shared policy (`pagefetch.AllowedContentType`, HTML and
XHTML only) enforced on both paths: the browser fallback captures the rendered
document's `document.contentType` and rejects non-HTML media the same way the
HTTP path does, closing the gap where a browser-rendered PDF or image was
indexed as if it were HTML. Full browser network-event interception of
intermediate redirect URLs remains a planned refinement on top of the dial-time
guard.

Tasks:

1. Enforce allowed schemes: default `http`, `https` only.
2. Block private, loopback, link-local and metadata IP ranges unless intranet mode explicitly allows.
3. Resolve DNS safely and re-check IP after redirects.
4. Enforce max redirects.
5. Enforce robots.txt unless profile disables it explicitly and admin confirms.
6. Enforce per-host delays and concurrency.
7. Enforce max response body bytes.
8. Enforce MIME allowlist.
9. Extend timeout budgets to browser navigation and any future finer body-read phases.

Acceptance:

- Unit tests cover SSRF protections.
- Redirect from public to private IP is blocked.
- Robots deny blocks fetch by default.

### CRAWL-05: HTML parsing and extraction

Status: partial implementation exists. The crawler extracts title, canonical
URL hints, meta descriptions, language, headings, visible text, and links from
HTML; resolves the canonical URL against the fetched page; carries page
description into document ingest metadata; splits followable and `rel=nofollow`
links; excludes nofollow links from frontier expansion and local outlink
evidence unless the crawl profile opts in; extracts normalized image URLs and
bounded alt text into document ingest metadata; produces document ingest, RWI
postings, URL metadata, and content hashes; and keeps extracted text bounded by
the node document store. Golden `testdata/` HTML fixtures now cover relative-link
resolution, malformed tag soup, UTF-8 multibyte content, and non-UTF-8
(`windows-1252`) declared through the HTTP `Content-Type` header only, alongside
the existing canonical, nofollow, and `<meta charset>` cases. Image
ranking/search is a search-side feature and remains planned separately.

Dependency candidates require ADR first. Prefer `golang.org/x/net/html` initially if enough. Add `goquery` only with ADR.

Tasks:

1. Extract title, meta description, canonical URL, language, headings, visible text and links.
2. Normalize links against base URL.
3. Respect `rel=nofollow` based on profile setting.
4. Extract image URLs and alt text for image metadata.
5. Produce RWI postings and URL metadata compatible with `yacymodel`.
6. Produce bounded snippets, not full body by default.
7. Hash content for dedupe.

Acceptance:

- Golden HTML fixtures cover canonical, nofollow, relative links, malformed HTML, UTF-8, non-UTF-8.
- Extracted text is bounded.
- No full body persists unless content cache enabled.

### CRAWL-06: Sitemap and sitelist support

Status: acceptance met. Local crawl dispatch accepts `startMode` values `url`,
`sitemap`, `sitelist`, and `robots`. The crawler fetches explicit sitemap and
sitelist seeds through the same proxied public-web admission path used for page
fetches, parses XML `urlset` documents, XML `sitemapindex` documents, and plain
text sitelists, carries sitemap `lastmod` as a crawl request hint, and imports at
most `YACYCRAWLER_SITEMAP_URL_LIMIT` URLs per seed before frontier admission. A
`robots` start fetches the seed host's `robots.txt`, extracts its `Sitemap:`
directives (`sitemap.ParseRobotsSitemaps`), and expands them through the same
bounded sitemap pipeline, failing open on a missing or unreadable file. Feeding
`lastmod` into a persistent-frontier recrawl scheduler is a distinct node-side
subsystem tracked as CRAWL-09.

Tasks:

1. Fetch and parse XML sitemaps. Done for explicit `sitemap` starts.
2. Support sitemap indexes. Done for bounded recursive expansion.
3. Respect lastmod as recrawl hint. Done: carried on expanded requests
   (`CrawlRequest.LastModified`); consumption by a recrawl scheduler is CRAWL-09.
4. Support plain text sitelist files. Done for explicit `sitelist` starts.
5. Bound number of URLs imported from sitemap/sitelist. Done through
   `YACYCRAWLER_SITEMAP_URL_LIMIT`.
6. Discover sitemap URLs from robots.txt. Done for `robots` starts.
7. Feed `lastmod` into persistent frontier recrawl scheduling. Deferred to
   CRAWL-09 (persistent frontier is a separate node-side subsystem).

Acceptance:

- Tests cover sitemap URL sets, sitemap indexes, invalid XML, huge sitemap
  truncation, fetch failures, bad expanded URLs, duplicate/capped sitemap files,
  invalid seed URLs, sitelist expansion, and robots.txt sitemap discovery
  (including fail-open on a missing file). Met.
- Sitelist mode creates independent roots. Met.

### CRAWL-09: Persistent frontier and recrawl scheduling

Split out of CRAWL-06. The crawler carries sitemap/robots `lastmod` onto
`CrawlRequest.LastModified`, but no component consumes it for recrawl decisions
yet; the crawler frontier is in-memory per run.

Tasks:

1. Add a durable, node-side frontier that records per-URL last-fetch time and
   next-eligible-recrawl time.
2. Feed sitemap `lastmod` and the profile `RecrawlIfOlder` into the recrawl
   schedule so unchanged pages are not refetched before they are due.
3. Survive node and crawler restarts without losing scheduled recrawls.

Acceptance:

- A page with a future `lastmod`-derived due time is not re-dispatched early.
- Scheduled recrawls survive a restart.

### CRAWL-07: Crawler worker hardening

Tasks:

1. Replace experimental assumptions with stable worker lifecycle.
2. Add durable consumer group behavior.
3. Add graceful shutdown: stop accepting new work, finish/park current fetches, commit offsets.
4. Add worker heartbeat to node.
5. Add metrics:
   - `yacy_crawler_jobs_active`
   - `yacy_crawler_fetches_total`
   - `yacy_crawler_fetch_failures_total`
   - `yacy_crawler_bytes_total`
   - `yacy_crawler_robots_denied_total`
   - `yacy_crawler_ingest_batches_total`
6. Add e2e test with node + broker + crawler.

Acceptance:

- Multiple crawler workers can share orders without duplicate fetch explosion.
- Backpressure from node slows crawler.

### CRAWL-08: YaCy `/Crawler_p.html` compatibility subset

Tasks:

1. Implement `/Crawler_p.html` parser for common GET/POST parameters from YaCy crawler API.
2. Map parameters into crawl profile/job model.
3. Return HTML for human use or JSON/XML if requested later.
4. Keep admin API as primary modern API.

Acceptance:

- Compatible with simple `crawlingstart`, `crawlingMode=url`, `crawlingURL`, `crawlingDepth`, `range`, `mustmatch`, `mustnotmatch`.
- Invalid requests show safe error and do not start jobs.

---

## 10. Phase 5 - Carbon web UI and admin API

Goal: build a modern admin/search interface comparable in breadth to original YaCy's web UI, using IBM Carbon.

### UI-01: Frontend foundation ADR and scaffold

Status: Done (ADR-0022, FTR-024). The admin console is a server-rendered Go
surface enhanced with a single vendored htmx, and the public portal is
server-rendered Go (ADR-0020) — no Vite/React/npm, so `make verify` stays Go-only
and the dependency surface is minimal. Delivered: the `internal/adminui` package
with embedded templates, Carbon-token CSS, and pinned `htmx.min.js`, mounted on the
operations listener behind the admin session guard.

Tasks:

1. Record the frontend stack in ADR-0022 (server-rendered Go `html/template` +
   hand-authored Carbon-token CSS + vendored htmx; no npm/Vite/React).
2. Create the `internal/adminui` package with an embedded base layout, Carbon-token
   CSS, and a pinned, vendored `htmx.min.js`, all embedded via `go:embed`.
3. Serve the console from the operations listener behind the admin session guard;
   handlers read internal state directly (no separate JSON API round-trip).
4. Keep `make verify` Go-only — the vendored assets ship in the binary, so there is
   no separate frontend lint/typecheck/build step.

Acceptance:

- `make verify` covers the console (Go tests) with no frontend toolchain.
- The Go server serves the console and its embedded assets; the public port never
  exposes it.
- No admin secrets or keys are embedded in any served asset.

### UI-02: Carbon app shell

Status: Done (FTR-024). The server-rendered Carbon shell (header, section side
nav with `aria-current`, content area, breadcrumb, keyboard skip-link, footer)
renders all nine admin routes plus a link to `/search`; unwired sections show a
controlled unavailable state and Overview shows a welcome, so the shell renders
without any backing API. Toasts and richer live states arrive with the sections
(UI-04..10) and htmx interactions.

Tasks:

1. Implement Carbon `Header`, `SideNav`, `Content`, `Breadcrumb`, `Theme` support.
2. Routes:
   - `/admin/overview`
   - `/admin/search`
   - `/admin/crawl`
   - `/admin/network`
   - `/admin/index`
   - `/admin/performance`
   - `/admin/configuration`
   - `/admin/security`
   - `/admin/logs`
   - `/search`
3. Add global notification/toast system.
4. Add loading/error/empty states.
5. Add accessibility baseline: keyboard nav, labels, focus management.

Acceptance:

- UI renders without API by showing controlled unavailable states.
- Axe or equivalent accessibility check is added if dependency ADR allows it.

### UI-03: Auth UI

Status: Partial (FTR-030). Done: server-rendered login (`/admin/login`),
first-run setup (`/admin/setup`), and CSRF-protected sign-out (`/admin/logout`)
in `internal/adminauth`, reusing the existing auth service; the session guard
redirects unauthenticated browsers to the login page and now accepts the CSRF
token from a `csrf_token` form field (not only the `X-CSRF-Token` header), with
`adminauth.CSRFTokenFromContext` exposing the token to the console for write
forms. Follow-ups: the in-console API-key management page and a password-change
page (API keys stay on the JSON auth API for now).

Tasks:

1. Login page.
2. First-run admin setup page.
3. Session check and route guard.
4. API key management page.
5. Password change page.

Acceptance:

- Admin pages require auth.
- Public search page remains accessible when configured public.
- API keys are shown only once at creation.

### UI-04: Overview dashboard

Status: Done (FTR-024). The Overview section renders live node status (documents,
RWI words, known peers, uptime, peer identity, and index-transfer totals) read
directly from the node status report via a decoupled `adminui.OverviewSource`, as
Carbon metric tiles, with an htmx partial (`/admin/overview/metrics`) that
auto-refreshes and a manual refresh button; it degrades to the same server-rendered
tiles without JavaScript and to a controlled unavailable state when unwired. This
established the console's data-provider plumbing (`adminui.Options`) reused by the
remaining sections.

Show:

- Node mode.
- Peer hash/name/type.
- Public endpoint reachability.
- RWI count.
- URL metadata count.
- Local documents count.
- Active crawl jobs.
- Active peers.
- DHT gates.
- Storage usage/quota.
- Queue health.
- Recent warnings.

Acceptance:

- Dashboard uses one summary endpoint plus focused detail calls.
- No polling faster than configured interval; prefer SSE later.

### UI-05: Search UI

Status: Done (FTR-025). The Search section runs queries against the node's shared
search core (same searcher as `/yacysearch.*` and the Tavily API, DDGS fallback
included) via a decoupled `adminui.SearchSource`; a server-rendered Carbon form
(`q`, `scope=global`) renders results with no client JavaScript, DDGS hits carry
the visible `[ddgs]` marker on this human surface, partial peer failures show as
notifications, and backend errors are generic. Query explain, richer filters, and
pagination remain follow-ups.

Features:

- Search box.
- Local/global source toggle.
- Tavily-compatible mode toggle for testing API shape.
- Filters: domain include/exclude, content domain, language, date range.
- Results list with title, URL, snippet, score/source tags.
- Result explain panel for admin users.
- Ranking settings page.
- Integration page showing OpenSearch, YaCy JSON/RSS, Tavily-compatible curl examples.

Acceptance:

- Search works through admin API and public `/search` route.
- Global search displays partial peer failures.

### UI-06: Crawler UI

Status: Partial (FTR-031). Done: the console Crawler section (`GET`/`POST
/admin/crawl`) — a CSRF-protected crawl-start form (seeds, name, mode, scope,
max depth) that reuses a new shared `crawldispatch.Dispatcher` (the JSON `POST
/crawl` endpoint now uses the same seam) to validate a profile and enqueue on the
crawl broker, confirming acceptance or showing a failure. Follow-ups: a
simple/expert toggle, a crawl-profile editor (CRAWL-03 expert fields), and a live
per-crawl monitor with results — the last is blocked on crawl-progress
observability the node does not yet expose.

Pages:

1. Simple crawl start:
   - Start URL.
   - Depth.
   - Domain/subpath/wide.
   - Max pages.
   - Start button.
2. Expert crawl start:
   - Full profile fields.
   - Regex validation.
   - Recrawl/delete/cache/index options.
3. Crawl monitor:
   - Running jobs.
   - Progress.
   - Queue sizes.
   - Fetch rate.
   - Failures.
   - Pause/resume/cancel.
4. Crawl results:
   - Indexed URLs.
   - Failed URLs.
   - Blocked by robots.
   - Duplicate/canonical skipped.
5. Crawl profiles editor.

Acceptance:

- Mirrors original YaCy breadth: simple start, expert start, monitor, results, profiles.
- Dangerous options require confirmation.

### UI-07: Network/P2P UI

Status: Partial (FTR-027). The Network section renders known/reachable peer counts,
DHT distribution gate state (with blocking reason), the per-gate results table, and
the freshest reachable peers (name, address, hash, age) read directly from the DHT
gate source and peer roster via a decoupled `adminui.NetworkSource`. Seed-list
management, per-peer detail, transfer history, and the network self-test control
remain follow-ups.

Pages:

1. Peer network overview:
   - Active peer count.
   - Known peer count.
   - Peer type distribution.
   - DHT transfer status.
2. Peer table:
   - Hash, name, type, address, flags, last seen, latency, RWI count, state.
   - Probe action.
   - Block/unblock action.
3. Seedlists:
   - Configured seedlist URLs.
   - Last import status.
   - Manual refresh.
4. DHT gates:
   - Each gate with pass/fail reason.
5. DHT transfers:
   - Inbound/outbound batches.
   - Failures.
   - Unknown URL reconciliations.
6. Self-test:
   - Public endpoint test.
   - `rwicount` callback test.

Acceptance:

- Comparable to YaCy network graphic/table/monitor in functional coverage, even if initial visualization is table-first.
- No secrets in peer data.

### UI-08: Index UI

Status: Partial (FTR-026). The Index section renders search-index statistics
(indexed document count, backend, last-updated) read directly from the node's
`SearchIndex` via a decoupled `adminui.IndexSource`, degrading to a controlled
unavailable state. Document/term browsing, blacklist management, and the schema
view remain follow-ups.

Pages:

1. Index stats:
   - RWI terms/postings.
   - URL metadata.
   - Local full-text index state if enabled.
   - Storage quota.
2. Document browser:
   - Search/filter indexed URLs.
   - View metadata, snippets, crawl history.
   - Delete by URL/domain/profile.
3. Term browser:
   - Inspect term/posting counts for admin debugging.
4. Blacklists:
   - Manage domain/path rules.
   - Import/export.
5. Schema/settings:
   - Show indexed fields.
   - Explain what is not stored by default.

Acceptance:

- Destructive actions require confirmation.
- Deletion triggers cleanup in RWI and metadata stores.

### UI-09: Performance and operations UI

Status: Partial (FTR-028). The Logs section renders recent structured events
(time, severity, category, name, message) newest-first from the in-memory events
recorder via a decoupled `adminui.LogsSource`, with an htmx auto-refresh partial
and severity tags, degrading to plain HTML without JavaScript. The separate
Performance section (queues, throughput, backup/restore controls) remains a
placeholder follow-up.

Pages:

- Memory/disk/runtime stats.
- Queue stats.
- Request latency and error rates.
- Crawl worker heartbeats.
- DHT transfer rates.
- Logs/events viewer.
- Backup/restore page.
- Shutdown/restart controls if supported.

Acceptance:

- Metrics are machine-readable and UI-readable.
- Restart/shutdown controls can be disabled by config.

### UI-10: Configuration UI

Status: Partial (FTR-032). Done: a read-only Configuration section (`GET
/admin/configuration`) showing the effective startup configuration grouped
(Node, Listeners, Storage, Search, Network policy, Crawler, Administrator) with
secrets redacted (search API key and admin password render only as
`Configured`/`Not set`). Follow-up: the write surface — the node has no
runtime-mutable configuration store yet, so runtime editing needs a config
persistence subsystem before it can be wired.

Pages:

1. First-run use case selection:
   - P2P peer.
   - Search portal.
   - Intranet.
2. Peer identity:
   - Peer hash.
   - Peer name.
   - Public host/port.
   - Network name.
3. Storage:
   - Quotas.
   - Raw content cache on/off.
   - Snippet retention.
4. Proxy:
   - HTTP proxy.
   - HTTPS proxy.
   - NO_PROXY.
5. Tavily:
   - Local/P2P compatible API settings.
   - Optional DDGS web-search fallback settings (enable/disable, backend, limits,
     safesearch, crawl-seeding).
   - Local search API key status without revealing key.
6. Security:
   - Admin password.
   - API keys.
   - CORS.
   - Public search enabled.
   - Rate limits.

Acceptance:

- Settings validate before save.
- Settings that require restart are clearly marked.

### UI-11: Public search portal (port 80)

Status: Done (FTR-029, ADR-0020). Off by default (`YAGO_PUBLIC_SEARCH_UI_ENABLED`),
a server-rendered `internal/publicportal` replaces the landing page at the public
root `/`: a minimal early-2000s-Yandex page (centered wordmark, one box+button,
plain results) that works without JavaScript and in legacy browsers, is responsive,
and is self-contained (inline styles). It queries the shared search core (local +
peers + DDGS fallback), shows the `[ddgs]` marker, exposes only search, and does
not log the query. OpenSearch/suggestions on the portal and SEC-05 privacy wiring
remain follow-ups.

A separate, admin-toggleable **public** search UI served on the node's public
HTTP port (`80` in appliance mode) — distinct from the authenticated Carbon admin
SPA (UI-02..UI-10). It is the anonymous front door for search-portal and intranet
deployments and is off by default (the "Public search enabled" runtime setting
and the `YAGO_PUBLIC_SEARCH_UI_ENABLED` toggle gate it). It renders the same
search that the admin UI and `/yacysearch.*` endpoints do, so DDGS-fallback hits
appear here with the visible `[ddgs]` marker (TAVILY-04); the Tavily-compatible
`POST /search` API stays a separate, unmarked drop-in surface.

Design and delivery constraints:

- **Look:** deliberately minimal, evoking early-2000s Yandex — a centered
  wordmark, one prominent search box with a single search button, almost no
  chrome above the fold, and a plain results list (title, URL, snippet, source
  tag) below the query on the results page. No dashboard density; this is a
  consumer search page, not an operator console.
- **Framework:** built on IBM Carbon (design tokens and, where they degrade
  gracefully, components) so it shares the visual language and theming of the
  admin UI.
- **Legacy-browser support:** the page must render and be usable in old browsers
  as far as Carbon allows. Because `@carbon/react` targets evergreen browsers,
  the portal is a progressively-enhanced, server-rendered surface: semantic HTML
  plus `@carbon/styles` tokens that work without JavaScript, with interactive
  Carbon enhancements layered on only where they degrade cleanly. Search must
  work with a plain form GET/POST and no client JavaScript.
- **Mobile:** responsive down to small phone widths; the search box and results
  reflow without horizontal scrolling.
- **Boundary:** the public portal exposes only search (and, if enabled, OpenSearch
  description and suggestions); it never exposes admin APIs, and it honors the
  SEC-05 privacy mode for query logging.
- The progressive-enhancement / how-far-to-push-Carbon-for-legacy-browsers
  decision and the public-vs-admin listener split are recorded in an ADR
  (ADR-0020) before build.

Acceptance:

- With the toggle off, port 80 serves no public search UI; with it on, `/` serves
  the Yandex-style portal and a query returns a results page.
- The portal renders a usable search box and results with JavaScript disabled.
- Layout is legible on a narrow mobile viewport without horizontal scrolling.
- DDGS-fallback results show the `[ddgs]` marker on the portal; the Tavily
  `POST /search` surface returns the same results unmarked.

---

### UI-12: OpenSearch integration for the public search portal

Make the public search portal (UI-11) discoverable and usable as a browser search
engine through OpenSearch, so a visitor can add the node to their browser's search
bar and search it directly.

Tasks:

1. Serve an OpenSearch description document (OSDD) describing the portal's search
   endpoint (the public root with the `q` parameter) and identity (short name from
   the brand, description, attribution), self-contained and cacheable.
2. Add the `<link rel="search" type="application/opensearchdescription+xml">`
   autodiscovery tag to the portal page so browsers offer to add it automatically.
3. Optionally expose an OpenSearch Suggestions endpoint (a JSON suggestions array)
   for typeahead, respecting the same privacy stance as the portal (no query
   logging by default, SEC-05).
4. Keep it public-surface-only (no admin data) and require no JavaScript for the
   core add-and-search flow. Only serve it when the portal is enabled (UI-11).

Acceptance:

- Browsers discover the portal as a search engine via the autodiscovery link and
  the served OSDD; adding it and searching hits the portal's public search.
- The OSDD and any suggestions endpoint expose only public search, honor the
  portal's privacy stance, and are covered by tests.

---

## 11. Phase 6 - Security, privacy and abuse controls

### SEC-01: Admin authentication

Tasks:

1. First-run setup requires creating admin user.
2. Store password using a modern password hash; dependency requires ADR. Prefer Argon2id via `golang.org/x/crypto/argon2` with stored params.
3. Session cookie is HttpOnly, Secure when HTTPS, SameSite=Lax or Strict.
4. CSRF protection for cookie-authenticated state changes.
5. Login rate limiting.

Acceptance:

- No default admin password.
- Failed login does not reveal whether username exists.
- Session invalidation works.

### SEC-02: API keys and scopes

Tasks:

1. Generate high-entropy API keys.
2. Store only hash and prefix.
3. Scopes:
   - `search:read`
   - `search:raw`
   - `crawl:write`
   - `admin:read`
   - `admin:write`
4. Per-key rate limits.
5. Last-used timestamp.

Acceptance:

- API key is visible only once.
- Logs show key prefix only if needed.

### SEC-03: CORS and public exposure

Tasks:

1. CORS disabled by default for admin endpoints.
2. Public search CORS separately configurable.
3. P2P endpoints remain accessible according to mode.
4. Admin UI can be bound to loopback while P2P binds public interface.

Acceptance:

- Tests cover CORS denied/allowed.
- Docs explain reverse proxy deployment.

### SEC-04: Crawler SSRF protection

Covered by CRAWL-04, but must be security-reviewed before release.

Acceptance:

- Private network crawl blocked in P2P/search portal mode by default.
- Intranet mode explicitly permits configured private CIDRs only.

### SEC-05: Privacy modes

Tasks:

1. Add setting for query logging:
   - off
   - aggregate only
   - full local logs
2. Add setting for the external DDGS web-search fallback (TAVILY-04) privacy:
   - disabled (default; no query leaves the node)
   - explicit per request (only when the request opts in)
   - enabled
3. Add retention settings for query logs, snippets, raw cache and crawl logs.

Acceptance:

- Default query logs are privacy-preserving and no query is sent to any external
  provider.
- UI explains when the external DDGS provider (DuckDuckGo/DDGS) receives a query
  and that discovered URLs may be crawled.

---

## 12. Phase 7 - Observability and operations

### OPS-01: Metrics

Expose Prometheus-style metrics or a documented JSON metrics endpoint. If adding Prometheus client dependency, create ADR first.

Metrics groups:

- HTTP request latency/errors.
- P2P peer counts and probe results.
- DHT inbound/outbound postings/batches/failures.
- Search latency/results/partial failures.
- Crawl jobs/fetches/failures/bytes.
- Storage usage/quota.
- Queue depths.
- Auth failures/rate limits.

Acceptance:

- Metrics endpoint can be disabled or auth-protected.
- Tests cover metric registration conflicts.

### OPS-02: Structured events

Tasks:

1. Create stable event names/constants for UI event log.
2. Persist recent events in bounded ring or storage table.
3. Severity: debug, info, warn, error.
4. Category: p2p, dht, search, crawl, storage, security, config.

Acceptance:

- UI can show recent important events without scraping logs.
- Bounded memory/disk.

### OPS-03: Backup and restore

Tasks:

1. Backup settings, peer identity, API key hashes, crawl profiles, index metadata and storage engine files safely.
2. Exclude volatile queues unless configured.
3. Restore requires stopped or maintenance mode.
4. Document restore compatibility across versions.

Acceptance:

- E2E backup/restore test with a small index.
- Backup does not reveal API key plaintext.

### OPS-04: Packaging

Tasks:

1. Update Dockerfiles for node, crawler and optional all-in-one dev setup to the
   shared `/opt/yago` layout: binaries under `/opt/yago/bin`, mutable data under
   `/opt/yago/data`, config under `/opt/yago/etc`, running as a non-root `yago`
   user.
2. Update `docker-compose.yml.example` with node, crawler, named volumes for
   `/opt/yago/data` and `/opt/yago/etc` (persistent across container recreation),
   and an optional reverse-proxy example.
3. Add a systemd unit example that runs the same binary against `/opt/yago`.
4. Add a config reference documenting every environment variable and its default,
   cross-linking `doc/configuration.md`.

Acceptance:

- `docker compose up` runs a local demo crawl/search with data and config
  surviving `docker compose down && up` via named volumes.
- Production docs explain the per-surface bind addresses (NET-01) and the
  `/opt/yago` layout shared by Docker, systemd, and the Debian package (OPS-05).

---

### OPS-05: Debian package builds

Ship installable `.deb` packages so operators can install yago on common
Debian-family distributions without Docker.

Tasks:

1. Add Debian packaging metadata (an `nfpm` config or `debian/` control files)
   that installs binaries to `/opt/yago/bin`, ships the systemd units, seeds
   default config under `/opt/yago/etc`, and creates a system `yago` user whose
   data lives under `/opt/yago/data`.
2. Target Debian 12 (bookworm), Debian 13 (trixie), and Ubuntu 24.04 LTS and
   newer; build per target in CI (matrix) and name artifacts by distro, arch, and
   version.
3. postinst/prerm scripts create the user and directories, set ownership, and
   enable but do not force-start the service; purge must not delete operator data.
4. Document `apt`/`dpkg -i` install, config location, and upgrade behavior.

Acceptance:

- A produced `.deb` installs on a clean Debian 12/13 and Ubuntu 24.04 container,
  the service starts against `/opt/yago`, and removal leaves operator data intact.
- No secrets or keys are baked into the package.

---

### OPS-06: Tag-driven release automation

Publish binaries, `.deb` packages, and generated release notes automatically when
a version tag is pushed.

Tasks:

1. Add a GitHub Actions workflow triggered on tags matching `v*` that builds the
   release binaries and the OPS-05 `.deb` matrix.
2. Generate release notes from the commits since the previous tag (grouped by the
   structured commit `Summary`/type where possible) and attach them to the Release.
3. Create a GitHub Release for the tag and upload the binaries and `.deb` files as
   assets; keep `container-image.yml` in sync so the tag also produces a matching
   image.
4. Reuse the standing verification gate (`make verify`) as a required check before
   any artifact is published.

Acceptance:

- Pushing `v0.0.1` produces a GitHub Release with generated notes plus binary and
  `.deb` assets for every target, and fails closed if `make verify` fails.
- Release notes list changes since the previous tag and known unsupported YaCy
  features (§20/§21).

---

### NET-01: Per-surface configurable bind addresses

Let operators choose the interface (IP) and port each externally reachable
surface binds to, so the public P2P port, the admin/ops surface, the Tavily API,
and the public search portal can be exposed or restricted independently — both
from the environment at boot and, at runtime, from the admin console.

Tasks:

1. Give each surface its own `host:port` bind setting via environment: the peer
   P2P listener, the admin/ops listener, and — where they are split out — the
   Tavily-compatible API and the public search portal (UI-11, default `:80`). The
   `host` part may be empty (all interfaces) or a specific IP (e.g. loopback).
2. Reconcile the §16.1 spec names with the implemented variables (see BRAND-02) so
   there is one documented, `YAGO_`-prefixed name per surface.
3. Keep sensible defaults: peer public, admin loopback-friendly, Tavily API and
   public portal off/opt-in as they exist today.
4. Validate bind strings at boot with clear errors and document exposure guidance.
5. Add an admin-console binding editor (a write surface on the Configuration
   section): enumerate the host's available network interfaces and their addresses
   — including loopback — and let the operator pick, per surface, a bind address
   from the discovered set (or "all interfaces") and a port. Persist the choice
   through the runtime settings store (CFG-01) so it overrides the environment
   default, and validate the address is bindable before saving.
6. Apply changes safely: re-bind the affected listener in place where possible, or
   clearly flag a restart-required state; add lock-out guardrails so the operator
   cannot bind the admin surface to an unreachable address and lose access.

Acceptance:

- Each surface binds to a specific IP and port independently through configuration,
  verified by tests over the config loader.
- Binding the admin surface to loopback is documented and works; the public search
  portal binds to its configured port only when enabled.
- The admin console lists the host's interfaces (including loopback) and can change
  a surface's bind address and port, persisted as a runtime override (CFG-01), with
  lock-out guardrails; covered by tests.

---

### CFG-01: Admin-writable runtime settings store

Provide a small, persisted settings store that the admin console can write and
that overrides the environment-derived defaults at runtime, so operator toggles
survive restarts and take effect without editing the environment. This is the
foundation the runtime write surfaces (UI-10 write, NET-01 binding editor, NET-02
toggles) build on.

Tasks:

1. Add a durable settings store (in the existing vault) holding a bounded set of
   operator-overridable settings, each with an explicit "unset -> use environment
   default" state, layered over the loaded `nodeConfig` (environment is the
   default; the store is the override).
2. Expose a typed read/write seam guarded by the admin session (admin:write scope,
   CSRF), reusing the UI-10 Configuration section for reads and the CSRF form
   pattern (FTR-030) for writes; never store or echo secrets in plaintext.
3. Apply overrides live where a subsystem can re-read them; otherwise surface a
   clear "restart required" state. Record each change as a structured event.
4. Keep it minimal and safe: whitelist exactly which settings are runtime-mutable
   (start with the public-portal toggle, the HTTPS redirect, and per-surface binds);
   do not make security-critical invariants silently changeable.

Acceptance:

- A whitelisted setting changed in the admin console persists across a restart and
  overrides its environment default; unset settings fall back to the environment.
- Writes require an admin session + CSRF; secrets are never persisted or shown in
  plaintext; changes emit structured events. Covered by tests.

---

### NET-02: Admin-toggleable HTTPS redirect and portal enablement

Let the operator turn on an HTTP->HTTPS redirect and enable the public search
portal from the admin console, overriding the environment defaults, built on the
runtime settings store (CFG-01).

Tasks:

1. Add an HTTP->HTTPS redirect, off by default: when enabled, a plain-HTTP request
   to a redirect-eligible surface receives a 301/308 to the `https://` origin
   (preserving path and query), toggled from the admin console (CFG-01). TLS
   termination itself is expected in front (reverse proxy) or via a configured
   certificate; only redirect when HTTPS is actually reachable.
2. Let the admin console enable/disable the public search portal (UI-11) as a
   runtime override of `YAGO_PUBLIC_SEARCH_UI_ENABLED` (CFG-01), mounting or
   unmounting the portal at the public root accordingly, with the environment value
   as the default when the override is unset.
3. Guardrails: never redirect the loopback admin surface into an unreachable state;
   validate before applying; emit a structured event on each toggle.

Acceptance:

- With the redirect enabled, an `http://` request is 301/308-redirected to the
  `https://` equivalent; with it off (default), no redirect happens. Tested.
- Toggling the portal in the admin console mounts/unmounts it at the public root and
  overrides the environment default; the override persists (CFG-01). Tested.

---

## 13. Testing strategy

### 13.1 Unit tests

Required for every package:

- DTO validation.
- Query parsing.
- Ranking determinism.
- Peer registry transitions.
- DHT batch construction.
- URL normalization.
- Crawler profile validation.
- SSRF protections.
- Auth/session/API key behavior.

### 13.2 Golden tests

Add golden fixtures for:

- YaCy P2P request/response forms.
- YaCy search JSON/RSS responses.
- Tavily-compatible search responses.
- HTML extraction.
- Sitemap parsing.

### 13.3 Integration tests

Add tests for:

- Node storage restart durability.
- Node + fake peer DHT transfer.
- Node + node DHT transfer.
- Node + crawler + fake broker ingest.
- Search local + remote merge.
- Admin API auth flow.

### 13.4 E2E tests

Behind e2e tags:

1. One node, one crawler, one broker:
   - Start crawl.
   - Fetch local test site.
   - Ingest.
   - Search via `/yacysearch.json` and `POST /search`.
2. Two Go nodes:
   - Node A crawls.
   - Node A distributes RWI to Node B.
   - Node B answers remote search.
3. Optional Java YaCy peer:
   - Use pinned `yacy/yacy_search_server` image if available.
   - Verify `hello`, `rwicount`, RWI transfer, URL transfer and search compatibility.

### 13.5 Frontend tests

- Component tests for major pages.
- API client tests with mocked responses.
- Playwright smoke tests:
  - first-run setup
  - login
  - start crawl
  - view crawl progress
  - search
  - view network peers
  - create API key

---

## 14. Detailed implementation queue for agents

Use this queue in order unless a human explicitly reprioritizes.

### Milestone A: P2P inbound correctness

1. FND-01 docs.
2. FND-02 ADR template.
3. FND-03 fixtures.
4. P2P-01 peer registry.
5. P2P-02 self-test.
6. P2P-03 inbound RWI durability.
7. P2P-04 URL metadata ingest.
8. P2P-05 remote search responder.

Ship when:

- Existing upstream features still work.
- A YaCy-compatible peer can identify this node as senior/DHT-capable.
- Local search over received RWI returns useful results.

### Milestone B: Search APIs

1. SEARCH-01 domain model.
2. SEARCH-02 parser.
3. SEARCH-03 local RWI lookup.
4. SEARCH-05 federated remote search.
5. SEARCH-06 YaCy JSON/RSS/OpenSearch APIs.
6. SEARCH-07 ranking/explain.

Ship when:

- `/yacysearch.json` and `/yacysearch.rss` are usable.
- `resource=local` and `resource=global` behave differently and correctly.

### Milestone C: Tavily compatibility

1. TAVILY-01 DTOs.
2. TAVILY-02 local/P2P adapter.
3. TAVILY-03 auth/scopes.
4. TAVILY-04 optional DDGS web-search fallback provider.
5. TAVILY-05 optional extract subset.
6. TAVILY-06 search-miss crawl seeding.

Ship when:

- API clients expecting the Tavily Search API can call `POST /search` and get useful results.
- No external provider or key is required for local/P2P mode.

### Milestone D: Crawler wiring

1. CRAWL-01 order producer.
2. CRAWL-02 ingest consumer.
3. CRAWL-03 profile model.
4. CRAWL-04 fetch safety.
5. CRAWL-05 extraction.
6. CRAWL-06 sitemap/sitelist.
7. CRAWL-07 worker hardening.
8. CRAWL-08 `/Crawler_p.html` subset.

Ship when:

- A local crawl makes documents searchable.
- Crawler obeys politeness and SSRF protections.
- Admin API can manage jobs.

### Milestone E: Outbound DHT

1. P2P-06 outbound distributor.
2. P2P-07 DHT gates data.
3. P2P-08 remote crawl safe policy.

Ship when:

- Two Go nodes exchange RWI/URL metadata.
- DHT gates are observable and explain why distribution is or is not running.

### Milestone F: Carbon UI

1. UI-01 scaffold.
2. UI-02 app shell.
3. UI-03 auth UI.
4. UI-04 overview.
5. UI-05 search UI.
6. UI-06 crawler UI.
7. UI-07 network UI.
8. UI-08 index UI.
9. UI-09 performance UI.
10. UI-10 configuration UI.

Ship when:

- Admin UI covers the same broad functional categories as original YaCy: search, crawl, data/index, configuration, network activity, performance.

### Milestone G: Release readiness

1. SEC-01 to SEC-05.
2. OPS-01 to OPS-04.
3. Full docs.
4. Full e2e.
5. Release notes.

Ship when:

- A fresh user can run Docker Compose, configure first-run admin, start a crawl, search, enable P2P and inspect DHT/network state.

---

## 15. Concrete API behavior details

### 15.1 `/yacysearch.json` compatibility subset

Request example:

```text
/yacysearch.json?query=example&resource=global&maximumRecords=10&startRecord=0&contentdom=text&verify=false&urlmaskfilter=.*&prefermaskfilter=
```

Behavior:

- `resource=local`: local index only.
- `resource=global`: local index plus remote YaCy peer fanout if enabled.
- `maximumRecords`: cap by auth state and config.
- `startRecord`: zero or one-based ambiguity must be tested against YaCy docs/fixtures; document chosen behavior.
- `verify=true`: if content cache exists, verify freshness; otherwise mark as unsupported/ignored with metadata. Do not fetch arbitrary result URLs during search unless explicitly configured.
- `nav=all`: return facets when implemented; until then return empty navigators.

Response should include:

- total results estimate.
- start index.
- items per page.
- query.
- result items with title/link/description/pubDate/host/path/file/guid-like URL hash where possible.
- partial failure metadata for global search.

### 15.2 Tavily-compatible `POST /search`

Request example:

```json
{
  "query": "golang yacy p2p search",
  "search_depth": "basic",
  "max_results": 10,
  "include_answer": false,
  "include_raw_content": false,
  "include_favicon": true,
  "include_domains": [],
  "exclude_domains": []
}
```

Response example shape:

```json
{
  "query": "golang yacy p2p search",
  "results": [
    {
      "title": "Example title",
      "url": "https://example.org/page",
      "content": "Bounded snippet or metadata summary.",
      "score": 0.73,
      "raw_content": null,
      "favicon": "https://example.org/favicon.ico",
      "images": []
    }
  ],
  "response_time": 0.123,
  "usage": {
    "credits": 0
  },
  "request_id": "generated-request-id"
}
```

Notes:

- `usage.credits` is local synthetic usage and stays `0`; the node calls no metered external provider. The DDGS web-search fallback is keyless and unmetered.
- `answer` appears only when requested.
- `raw_content` is omitted unless stored extracted document text is available and requested.
- `safe_search` is not a magic classifier. Map it to local blacklist/content policy and document limitations.

### 15.3 Admin API response envelope

Use a consistent envelope for admin APIs:

```json
{
  "data": {},
  "warnings": [],
  "request_id": "generated-request-id"
}
```

Errors:

```json
{
  "error": {
    "code": "invalid_crawl_profile",
    "message": "Crawl depth exceeds configured maximum.",
    "fields": {
      "depth": "max_allowed_4"
    }
  },
  "request_id": "generated-request-id"
}
```

---

## 16. Configuration plan

Prefer environment variables for boot-critical settings and persistent settings for runtime-configurable values.

### 16.1 Boot-critical environment

- `YACY_NODE_DATA_DIR`.
- `YACY_NODE_BIND_ADDR`.
- `YACY_NODE_PUBLIC_HOST`.
- `YACY_NODE_PUBLIC_PORT`.
- `YACY_NODE_PEER_HASH`.
- `YACY_NODE_PEER_NAME`.
- `YACY_NODE_MODE`.
- `YACY_NODE_ADMIN_BIND_ADDR` if split binding is supported.
- `YACY_CRAWL_RPC_ADDR` for crawler integration.

> These names are aspirational and predate the implementation, which uses
> `YACY_PEER_ADDR` / `YACY_OPS_ADDR` and a growing set of `YAGO_*` variables.
> BRAND-02 converges them onto one documented `YAGO_*` name per setting, and
> NET-01 adds a per-surface `host:port` bind for the peer, admin, Tavily API, and
> public search portal listeners.

### 16.2 Optional DDGS web-search fallback

- `YAGO_WEB_FALLBACK_ENABLED=false`.
- `YAGO_WEB_FALLBACK_PROVIDER=ddgs`.
- `YAGO_WEB_FALLBACK_BACKEND=auto` (`auto` excludes DuckDuckGo — Mojeek then Bing;
  `mojeek`, `bing`, or `duckduckgo` select one engine explicitly; see ADR-0021).
- `YAGO_WEB_FALLBACK_MAX_RESULTS`.
- `YAGO_WEB_FALLBACK_TIMEOUT`.
- `YAGO_WEB_FALLBACK_SAFESEARCH`.
- `YAGO_WEB_FALLBACK_CACHE_TTL` (short-TTL provider response cache).
- `YAGO_WEB_FALLBACK_SEED_CRAWL=false` (seed the crawler from discovered URLs;
  effective only when crawling is enabled).
- `YAGO_WEB_FALLBACK_SEED_DEPTH=1` (shallow seed-crawl depth, 0–8).
- `YAGO_WEB_FALLBACK_SEED_MAX_PAGES=20` (per-host page cap for seeded orders).

### 16.3 Runtime settings

- P2P enabled.
- Seedlist URLs.
- DHT distribution enabled.
- Remote search enabled.
- Remote crawl enabled.
- Public search enabled.
- Public search portal UI enabled (Yandex-style, served on the public port; UI-11).
- Tavily-compatible endpoint enabled.
- Raw content cache enabled.
- Snippet retention.
- Crawl defaults and maxima.
- Rate limits.
- Query log retention.
- Storage quotas.

---

## 17. Risk register

### Risk: claiming full YaCy compatibility too early

Mitigation:

- Maintain `compatibility.md` with exact supported subset.
- Return clear 501/unsupported errors for endpoints not done.

### Risk: P2P abuse or poisoning

Mitigation:

- Quarantine peers with malformed data.
- Bound every request.
- Validate URL metadata.
- Deduplicate and score by local policy.
- Add blacklists and peer blocklist.

### Risk: crawler SSRF

Mitigation:

- Block private networks by default.
- Re-check redirects and DNS.
- Require intranet mode or explicit CIDR allowlist.

### Risk: storage explosion

Mitigation:

- Quotas for postings, URL metadata, snippets, raw content and logs.
- Backpressure before disk is full.
- Delete tools by domain/profile/age.

### Risk: external DDGS privacy leakage and crawl amplification

Mitigation:

- DDGS web-search fallback disabled by default; queries never leave the node until
  an operator opts in, and the SEC-05 privacy mode can require per-request opt-in.
- Explicit UI indicators when a query reaches DuckDuckGo/DDGS and when discovered
  URLs may be crawled.
- Outbound queries pass the egress guard; fallback responses are rate-limited,
  backed off, and cached.
- Search-miss crawl seeding uses a conservative, robots-respecting profile with
  per-host caps and URL deduplication so it cannot amplify into unbounded crawls.
- Redacted logs.
- Per-request/provider selection.

### Risk: frontend dependency sprawl

Mitigation:

- ADR before frontend dependencies.
- Keep frontend static and minimal.
- Avoid heavy charting until necessary.

### Risk: huge generated rewrites

Mitigation:

- Use task IDs.
- One task per patch.
- Tests and `make verify` gate.
- Avoid broad renames until module rename ADR.

---

## 18. Implementation task briefs

Use these briefs when splitting roadmap work into issues, patches, or pull requests.

### 18.1 General task brief

```text
Implement only task <TASK-ID> from PLAN.md.
Do not implement neighboring tasks.
Follow existing package boundaries and OCP: add narrow seams instead of growing existing files.
Do not add third-party dependencies unless the task explicitly includes an ADR and you write it first.
Do not add code comments unless PLAN.md or the human explicitly asks.
Add tests for required behavior.
Run the narrow tests, then make verify.
When done, summarize changed files, behavior, tests run, and any remaining compatibility gaps.
```

### 18.2 P2P task brief

```text
Implement <P2P-TASK-ID> from PLAN.md.
Before coding, inspect yacymodel, yacyproto and yacynode existing /yacy endpoint handling.
Keep wire protocol DTOs at the edge.
Add golden fixtures for YaCy-compatible form/key=value behavior.
Do not change public wire format unless tests prove existing behavior was wrong.
Run make verify.
```

### 18.3 Search task brief

```text
Implement <SEARCH-TASK-ID> from PLAN.md.
Create transport-independent searchcore types first.
Then adapt YaCy/Tavily/admin transports to searchcore.
Search must have bounded deadlines, bounded result counts and deterministic ranking.
Add tests for local, global, empty, malformed and partial-failure cases.
Run make verify.
```

### 18.4 Crawler task brief

```text
Implement <CRAWL-TASK-ID> from PLAN.md.
Treat crawler input as untrusted.
Preserve privacy defaults: no full raw body persistence unless explicitly enabled.
Enforce SSRF, robots, redirect, body-size and deadline controls.
Add fixtures for HTML/sitemap/profile validation behavior.
Run make verify.
```

### 18.5 UI task brief

```text
Implement <UI-TASK-ID> from PLAN.md.
Use IBM Carbon React components.
Keep the frontend static-build friendly and served by the Go node.
Add TypeScript types for API DTOs.
Add loading, error and empty states.
Do not hardcode secrets or runtime config into the build.
Extend make verify with frontend checks if not already done.
```

### 18.6 Security task brief

```text
Implement <SEC-TASK-ID> from PLAN.md.
Assume all public HTTP input is hostile.
Never log secrets, cookies, auth headers or API keys.
Add tests for denial paths, rate limits and scope failures.
Document user-visible security behavior.
Run make verify.
```

---

## 19. First three recommended patches

Start here.

### Patch 1

Task: FND-01 + FND-02 only.

Expected files:

- `FORK.md`
- `yacynode/doc/fork-roadmap.md`
- `yacynode/doc/compatibility.md`
- `yacynode/doc/adr/0000-template.md`
- `yacynode/doc/adr/README.md`

No Go code changes.

### Patch 2

Task: FND-03.

Expected files:

- `yacynode/test/fixtures/yacywire/...`
- tests in existing protocol packages.

No behavior changes unless a round-trip bug is exposed and fixed narrowly.

### Patch 3

Task: P2P-01.

Expected files:

- `yacynode/internal/peerregistry/...`
- package tests.
- possible storage interface addition behind narrow seam.
- docs update only if operator behavior changes.

Do not implement outbound DHT in Patch 3.

---

## 20. Compatibility language for README/release notes

Use this wording until full compatibility is proven:

```text
This project is a Go YaCy-compatible peer focused on RWI/DHT interoperability, local crawling and search, and agent-friendly search APIs. It is not a drop-in replacement for the Java YaCy Search Server. Compatibility is implemented endpoint by endpoint and documented in yacynode/doc/compatibility.md.
```

For Tavily:

```text
The /search endpoint implements a Tavily-compatible JSON shape for local/P2P search. It calls no external search provider unless the optional DDGS web-search fallback is enabled by an operator; even then it is used only when local and federated search return nothing, its results are tagged [ddgs], and it needs no API key.
```

For crawler:

```text
The crawler stores URL metadata, search references and bounded snippets by default. Full raw content caching is optional, quota-limited and disabled by default.
```

---

## 21. Final release checklist

Before tagging a release:

- [ ] `make verify` passes.
- [ ] Frontend lint/typecheck/test/build passes through `make verify`.
- [ ] E2E node + crawler + broker passes.
- [ ] E2E two-node DHT transfer passes.
- [ ] Optional Java YaCy compatibility test documented and passing if environment supports it.
- [ ] `compatibility.md` is current.
- [ ] Security docs are current.
- [ ] Docker Compose example works from clean checkout.
- [ ] First-run admin setup works.
- [ ] Public search can be disabled.
- [ ] Admin bind can be private/loopback.
- [ ] P2P bind/public endpoint documented.
- [ ] DDGS web-search fallback disabled by default.
- [ ] No secrets in logs.
- [ ] AGPL source/legal notice visible in UI.
- [ ] Release notes list known unsupported YaCy features.
- [ ] Debian packages build and install on supported distros (OPS-05).
- [ ] Tag-driven release publishes generated notes and artifacts (OPS-06).

## 22. Rebrand (yacy -> yago)

Unify the project's own naming on `yago` across Go modules, files, environment
variables, and containers, while preserving every wire-level YaCy compatibility
point. This is presentation and naming only: nothing a Java YaCy peer or a YaCy
client observes on the network may change.

Invariants for every BRAND task (must NOT be renamed):

- YaCy protocol endpoint paths: `/yacy/*` (hello, transferURL, transferRWI,
  crawlReceipt, list, ...) and the human search paths
  `/yacysearch.{json,rss,html}`.
- The `/Crawler_p.html` Unsupported-by-design marker (CRAWL-08) and any other
  YaCy-named path kept for client compatibility.
- On-wire vocabulary and DTO field names in `yacyproto` payloads.
- The legacy `yacy-rwi.db` data-file open fallback (existing installs).
- The `YaCy`/AGPL attribution required by the license.

### BRAND-01: Rename Go modules, import paths, and directories

Tasks:

1. Rename the module directories and `go.mod` module paths
   `github.com/D4rk4/yago/yacy{node,model,proto,crawlcontract,egress,crawler}` to
   `.../yago{node,model,proto,crawlcontract,egress,crawler}`, and update `go.work`,
   every import, and `go.work.sum`/`go.sum`.
2. Update `.go-arch-lint.yml` vendor globs, the `Makefile` module lists, Dockerfile
   build paths, and any tooling that hard-codes the old paths.
3. Do this as one mechanical, scripted change per module with `make verify` green
   after each, isolated from any behavior change.
4. Record the rename and the preserved-compatibility invariants in an ADR.

Acceptance:

- No `yacy`-prefixed Go module path or directory remains; `make verify` is green.
- Every invariant above is untouched; the Java-compatibility e2e still passes.

### BRAND-02: Rename environment variables YACY_* -> YAGO_*

Tasks:

1. Rename every `YACY_*` variable to a `YAGO_*` name and converge the §16.1 spec
   names with the implemented ones (one documented name per setting).
2. Decide and document the compatibility policy: accept the old `YACY_*` names as
   deprecated aliases for one release with a startup warning, or a clean break;
   default to the alias grace period unless a clean break is requested.
3. Update `doc/configuration.md`, the compose/systemd/deb examples, and tests.

Acceptance:

- Configuration loads through `YAGO_*` names (plus documented deprecated aliases
  if the grace period is chosen); docs and examples match the code.
- No endpoint path or wire field is affected.

### BRAND-03: Rename containers, images, binaries, and default paths

Tasks:

1. Rename container images, compose service names, and binaries from `yacy*` to
   `yago*`, aligned with the `/opt/yago` layout (OPS-04/05); keep the legacy
   `yacy-rwi.db` open fallback.
2. Update Dockerfiles, `docker-compose.yml.example`, systemd units, and the Debian
   packaging metadata.
3. Update README and docs image/command references.

Acceptance:

- Built images, compose services, and binaries use `yago*` names; a clean
  `docker compose up` works.
- Existing data directories containing `yacy-rwi.db` still open.

### BRAND-04: Adopt the "YagoSeek" public brand

Establish **YagoSeek** as the public-facing brand while keeping `yago` as the
binary/module/CLI name and `github.com/D4rk4/yago` as the repository. This is
public naming and copy only; it changes no wire-compatibility invariant above,
nor the `yago` command or module paths (BRAND-01..03).

Naming:

- Brand: **YagoSeek**. CLI/binary and Go module short name: `yago`. Repository:
  `github.com/D4rk4/yago`. Domain: `yagoseek.dev`, with `docs.`, `api.`,
  `demo.` (public demo), and `status.` subdomains.

Tasks:

1. Rewrite the README lede to introduce YagoSeek:
   "YagoSeek is a self-hosted, YaCy-compatible P2P search node written in Go. It
   provides crawling, indexing, local search, federated peer discovery, and a
   Tavily-like Search API for applications, agents, and private search
   deployments." Include the hero copy ("Open search infrastructure for
   developers.") and the tagline "YagoSeek — your own federated search node."
2. Use YagoSeek as the product name in README and docs and `yago` in commands
   (`go install github.com/D4rk4/yago/cmd/yago@latest`, `yago node start`); keep
   the YaCy compatibility statement and the AGPL attribution intact.
3. Sequence after BRAND-01..03 so the README's `yago` commands and module paths are
   real; reference the `docs./api./demo./status.` domain structure.

Acceptance:

- README and docs present YagoSeek as the brand and `yago` as the command, with
  accurate install/run instructions once BRAND-01..03 land.
- No wire endpoint, DTO field, data-file fallback, or AGPL/YaCy attribution
  changes.
