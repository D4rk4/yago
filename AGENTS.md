Modern Go YaCy-compatible P2P search node. Spec: `yacynode/doc/specification.md`.

Project language: Use English in code, documentation, generated project files, commits, and user-visible text. Exceptions are exact upstream protocol fixtures, exact legal license text, and externally supplied data that must remain verbatim.

Workspace instructions: This root `AGENTS.md` is the single canonical agent instruction file for the workspace. Do not add nested instruction files or tool-specific mirrors.

Project direction: `yago` is not a literal port of Java YaCy, Solr, Lucene, or Kelondro internals. Preserve YaCy wire protocol compatibility and observable peer behavior, keep RWI as the YaCy P2P exchange format, and build a modern Go search/crawler/P2P stack around explicit component boundaries. Java, Solr, Lucene, Kelondro, and servlet-style YaCy UI must not become required internal runtime dependencies.

Search architecture rule: RWI is a compatibility and exchange layer, not the primary local search engine. Local search must move toward a document store plus a full-text search backend abstraction. Use RWI for YaCy-compatible exchange, DHT behavior, and protocol interop; do not rely on RWI as the only local index for snippets, phrase/proximity, Tavily-compatible raw content, answer generation, or semantic reranking.

Target runtime architecture:

```text
yago-node
  - YaCy /yacy/* compatibility
  - peer roster, seedlists, liveness, DHT inbound/outbound
  - RWI vault + URL metadata vault
  - P2P policy, quotas, metrics

yago-searchd
  - local full-text index
  - default recommended backend: Tantivy sidecar
  - fallback pure-Go backend: Bleve or Bluge
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

Architecture Agent: Owns boundaries between `yago-node`, `yago-searchd`, `yago-crawld`, and `yago-admin-ui`; prevents Solr/Kelondro/Java YaCy storage and servlet UI assumptions from becoming internal dependencies; defines interfaces between node/search/crawler/document store/admin APIs; plans migration from the current RWI node to a complete search appliance.

P2P Compatibility Agent: Owns `/yacy/hello.html`, seedlists, peer liveness, `/yacy/query.html`, `/yacy/transferRWI.html`, `/yacy/transferURL.html`, `/yacy/search.html`, `/yacy/urls.xml`, inbound transfer, outbound DHT scheduler, batching, retry, peer selection, peer reputation, rate limits, and the compatibility matrix against Java YaCy. Legacy YaCy P2P compatibility must remain stable even if native `yago-v2` P2P is introduced later.

Native P2P v2 Agent: Owns optional experimental go-libp2p, Kademlia/provider records, signed peer metadata, peer discovery, result federation, and gossipsub/events if needed. Native P2P v2 must be optional/experimental and must not break `/yacy/*` compatibility.

Search Core Agent: Owns full-text backend abstraction, document store integration, Tantivy sidecar integration as the preferred production backend, Bleve/Bluge pure-Go fallback, BM25, field boosts (`title > headings > anchor text > body`), phrase/proximity search through positional indexes, snippets/highlighting, language analyzers, filters/facets, freshness signals, and domain quality signals. Target interface:

```go
type SearchIndex interface {
    Index(ctx context.Context, doc Document) error
    Delete(ctx context.Context, docID string) error
    Search(ctx context.Context, req SearchRequest) (SearchResultSet, error)
    Stats(ctx context.Context) (IndexStats, error)
}
```

Document Store Agent: Owns storage for canonical URL, normalized URL, title, headings, extracted text, optional raw HTML/WARC reference, language, content type, fetch status, fetch timestamps, content hash, outlinks, inlinks/anchor text when available, and metadata for snippets and Tavily `raw_content`. Without a document store, high-quality snippets, phrase search, Tavily-compatible `include_raw_content`, answer generation, and semantic reranking are not achievable.

Tavily API Agent: Owns Tavily-compatible `POST /search`. Minimum request fields are `query`, `search_depth`, `max_results`, `include_answer`, `include_raw_content`, `include_domains`, `exclude_domains`, `topic`, `time_range`, and `safe_search`. Response shape follows `query`, optional `answer`, `results[]` with `title`, `url`, `content`, optional `raw_content`, `score`, optional `published_date`, and `response_time`. Provider order is local full-text, optional local semantic/vector, yago/yacy peers, then optional upstream Tavily provider only when explicitly configured. Do not turn Tavily API into a simple proxy.

YaCy Search API Agent: Owns `/yacysearch.json`, `/yacysearch.rss`, OpenSearch-style compatibility, `resource=local|global`, query parameter translation, feasible filters by site/domain/filetype/protocol/language/date, and compatibility notes for unsupported YaCy-specific ranking/profile fields.

Crawler Agent: Owns a production-grade crawler with persistent frontier, states `queued`, `fetching`, `fetched`, `failed`, `deferred`, `blocked`, robots.txt, crawl-delay, per-host token bucket, sitemap ingestion, URL normalization, redirects, canonical link handling, content hash deduplication, HTTP fast fetch path, browser fallback only when needed, content extraction, MIME/size/time limits, retry policy, recrawl scheduling, backpressure from index/searchd, and NATS JetStream integration if used.

Security Agent: Owns SSRF protection, denial of private CIDR/localhost/link-local/multicast/metadata IPs, DNS rebinding protection, crawl sandboxing, max body size, max redirects, allowed schemes, API auth, admin auth, remote crawl default-deny, peer quotas, and spam/index poisoning protections.

Admin UI Agent: Owns IBM Carbon admin UI. UI categories should be comparable to original YaCy administration without copying the legacy servlet UI: Search console, Crawl profiles, Crawl queue, P2P network, Peer details, Seedlists, DHT transfer in/out, Index/storage, Document browser, Search backend status, Tavily-compatible API settings, Security settings, Remote crawl policy, Metrics/performance, Logs/events, and Configuration. Use IBM Carbon with React via Next.js or Vite, and prefer a typed API client generated from OpenAPI when practical.

Observability Agent: Owns `/health`, `/ready`, Prometheus metrics, structured logs, tracing if practical, crawl queue depth, fetch rate, index rate, search latency, P2P transfer success/failure, peer reputation metrics, storage usage, and compaction stats.

QA / Compatibility Agent: Owns unit tests, integration tests, e2e tests with local multi-peer setup, race tests, golden tests for YaCy wire protocol, compatibility matrix against Java YaCy, crawler safety tests, search relevance fixtures, Tavily API contract tests, and UI smoke tests.

Roadmap priorities:

- P0: Align documentation status across README, FEATURES, crawler README, and plan files; add document store; add search backend abstraction; add first full-text backend; prefer Tantivy sidecar for the production profile; add Bleve/Bluge fallback for pure-Go/simple profile; implement local Tavily-compatible `/search`; implement `/yacysearch.json` and `/yacysearch.rss` on the search core; make snippets come from document store, not only URL metadata; keep RWI generation and YaCy P2P compatibility working.
- P1 crawler: persistent frontier, robots/politeness, sitemap ingestion, canonicalization, deduplication, browser fallback, backpressure, SSRF protection, and recrawl scheduler.
- P1 P2P/DHT: outbound DHT scheduler, peer selection, batching, retry policy, redundancy, delete-after-success policy where safe, peer reputation, strict remote crawl policy, and compatibility status endpoint.
- P1 admin UI: Search console, Crawl management, P2P network, Index/storage, Security, Metrics, Logs/events, and Settings.
- P2: positional phrase/proximity search, domain quality/freshness ranking, optional Qdrant vector/semantic rerank, optional local answer generation from snippets, federated result fusion across peers, and native libp2p-based `yago-v2` network.

Strategic guardrails: Do not reimplement Java YaCy internals blindly. Do not introduce JVM, Solr, Lucene, or Kelondro as required runtime dependencies. Do not make upstream Tavily mandatory. Do not use RWI as the only local search index. Do not store raw crawled content without size limits and security policy. Do not allow remote crawl by default. Do not crawl private/local networks. Do not let UI drive unstable internal APIs directly; expose typed admin APIs. Do not break existing YaCy wire compatibility endpoints. Do not delete existing compatibility docs; update them with status notes instead.

Continuity: Maintain one workspace ledger in `CONTINUITY.md`. At the start of every assistant turn, read it, update it with the current goal, constraints, decisions, progress state, and important tool outcomes, then continue the work. Keep it short: facts only, bullets preferred, uncertainty marked `UNCONFIRMED`. Keep `functions.update_plan` for short-term execution scaffolding and `CONTINUITY.md` for durable session state. Replies start with a brief Ledger Snapshot containing Goal, Now/Next, and Open Questions. The ledger keeps these headings: Goal (incl. success criteria), Constraints/Assumptions, Key decisions, State, Done, Now, Next, Open questions, Working set (files/ids/commands).

Feature catalog: Maintain `FEATURES.md` in the workspace root. It describes project capabilities side by side across `yacynode`, `yacycrawler`, `yacycrawlcontract`, `yacymodel`, and `yacyproto` where relevant. When adding a feature or changing behavior, update the affected capability, surface, status, behavior summary, and relevant files/tests.

Code structure: Follow OCP. Add features in new files and connect them through the smallest seam; do not grow existing files.

Module boundaries: Keep YaCy value types in `yacymodel`, wire protocol DTOs and endpoint vocabulary in `yacyproto`, node runtime/storage/P2P/search/ops behavior in `yacynode`, crawler worker behavior in `yacycrawler`, and node-crawler message contracts in `yacycrawlcontract`. Do not mix crawler runtime code into the node except through the contract and narrow node-side crawl orchestration.

Logging: Use stable message constants; put variable data in key/value fields. Happy paths: DEBUG. Sad paths: WARN if recoverable, ERROR if action is needed.

Comments: No comments without explicit user approval. Use naming and structure instead. Put required prose in `yacynode/doc/` or the relevant module README. Godoc package docs are allowed. If a comment seems unavoidable, ask first.

Single source of truth: Do not duplicate facts in comments, errors, logs, or similar text when they already exist in constants, config, docs, or protocol definitions.

Documentation: Each doc is self-contained, concise, plain-language, and user-facing. Links are for navigation only. Avoid cross-doc dependencies, duplicate facts, jargon, implementation details, and rationale. Behavior changes update the relevant module README, `yacynode/doc/`, `FEATURES.md`, and `CONTINUITY.md`; update a root README if one is introduced.

Naming: Every package, file, type, interface, port, function, method, field, and variable has one bounded responsibility. Prefer explicit bounded names over short generic names. Never use `util.go`, `helpers.go`, `handler.go`, or `types.go`. Reject umbrella names such as Store, Manager, Service, Handler, Util, or catch-all domain names like Distribution*. If the boundary cannot be stated in one sentence, fix the abstraction.

Naming style: Name a thing for what it is in the problem domain, never for how it is built or what it is for. Use the words a domain expert would say out loud; the same word holds in conversation, code, and docs. Strip implementation terms such as count, map, hash, digest, and buffer, and destination terms such as shared, peer, abstract, and response when they are not the domain noun. Spell names in full, readable English; length is free, abbreviation is not. Confine protocol- and transport-specific vocabulary to the edge that translates to and from it; inner code speaks plain domain language. Test every name by reading it aloud in a sentence to someone who knows the domain but not the code.

Dependencies: Record each new third-party dependency in its own ADR before use.

Version pinning: Pin all versions. Runtime deps: `go.mod`. Build/lint tools: Go tool directives in `go.mod` or the pinned tool lock. `make verify` uses only pinned tools, never PATH versions.

Security scanning: Feature closure uses pinned Dockerized scanners, never host PATH installs. Run Semgrep as `docker run --rm -v "${PWD}:/src:ro" -w /src semgrep/semgrep:1.168.0 semgrep scan --config auto --error`. Run Trivy after container images are built: first scan the source tree with `docker run --rm -v "${PWD}:/src:ro" -v trivy-cache:/root/.cache aquasec/trivy:0.72.0 fs --scanners vuln,secret,misconfig --exit-code 1 --severity HIGH,CRITICAL /src`, then scan each built project image with `docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v trivy-cache:/root/.cache aquasec/trivy:0.72.0 image --scanners vuln,secret,misconfig --image-config-scanners secret,misconfig --exit-code 1 --severity HIGH,CRITICAL <image>`.

Research: During planning for every task, do a short internet and arXiv research pass for relevant best practices, recent research, and implementation options, including but not limited to optimal architecture. Prefer primary sources such as specifications, official docs, source code, standards, and papers. If internet access is unavailable, record that in `CONTINUITY.md` and proceed from local context. Research does not create runtime internet dependencies.

Semantic behavior: Do not fix search, crawl routing, ranking, evidence selection, or compatibility behavior with ad hoc word lists, vendor facts, localized synonym buckets, or regexes that recognize specific meanings. Prefer protocol/data structures, parsed metadata, bounded model or ranking signals where such systems exist, and evidence from stored data. Regex is allowed for syntax-level parsing, protocol formats, numeric/unit tokenization, stable identifier normalization, security redaction, and file/URL/schema handling.

Testing: Code lands with tests. Pure documentation/configuration changes need lightweight validation only. For code changes, run focused tests first when useful, then `make verify`. Record exact commands and results in `CONTINUITY.md` and the final response. If a test cannot be added or run, state the concrete reason and residual risk.

Coverage: If coverage drops, first remove or refactor code. Find uncovered statements/branches and ask whether they should exist. Delete dead or defensive-only code, collapse unexercised branches, or replace several paths with one covered path. Add tests only for required behavior. Filler tests written only to raise coverage fail the change.

Commits: Every commit needs both a short subject and a body. The body must include `Motivation:`, `Summary:`, `What's new:`, and `Validation:` sections when applicable. Use real line breaks in commit bodies; do not embed literal `\n` escape sequences. When creating commits from the shell, use repeated `-m` arguments, an editor, or a commit message file so formatting is preserved. Keep commit text in English.

Feature closure: Closing a feature requires the tests to be written with the code, then a full test run, Dockerized Semgrep, Dockerized Trivy source and container-image scans, container builds, sanity tests, and smoke tests. If all feature-closure checks pass, commit the change and push it to `main`.

Gate: `make verify` is the mandatory code gate. A code change is not done until it is green. Feature closure adds the required Dockerized Semgrep, Dockerized Trivy, container, sanity, smoke, commit, and push checks.
