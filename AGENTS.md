Modern Go YaCy-compatible P2P search node. Spec: `yagonode/doc/specification.md`.

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

Architecture Agent: Owns boundaries between `yago-node`, `yago-searchd`, `yago-crawld`, and `yago-admin-ui`; prevents Solr/Kelondro/Java YaCy storage and servlet UI assumptions from becoming internal dependencies; defines interfaces between node/search/crawler/document store/admin APIs; plans migration from the current RWI node to a complete search appliance.

P2P Compatibility Agent: Owns `/yacy/hello.html`, seedlists, peer liveness, `/yacy/query.html`, `/yacy/transferRWI.html`, `/yacy/transferURL.html`, `/yacy/search.html`, `/yacy/urls.xml`, inbound transfer, outbound DHT scheduler, batching, retry, peer selection, peer reputation, rate limits, and the compatibility matrix against Java YaCy. Legacy YaCy P2P compatibility must remain stable even if native `yago-v2` P2P is introduced later.

Native P2P v2 Agent: Owns optional experimental go-libp2p, Kademlia/provider records, signed peer metadata, peer discovery, result federation, and gossipsub/events if needed. Native P2P v2 must be optional/experimental and must not break `/yacy/*` compatibility.

Search Core Agent: Owns full-text backend abstraction, document store integration, the embedded pure-Go Bleve backend tuned for web search, BM25, field boosts (`title > headings > anchor text > body`), phrase/proximity search through positional indexes, snippets/highlighting, language analyzers, filters/facets, freshness signals, and domain quality signals. Target interface:

```go
type SearchIndex interface {
    Index(ctx context.Context, doc Document) error
    Delete(ctx context.Context, docID string) error
    Search(ctx context.Context, req SearchRequest) (SearchResultSet, error)
    Stats(ctx context.Context) (IndexStats, error)
}
```

Document Store Agent: Owns storage for canonical URL, normalized URL, title, headings, extracted text, optional raw HTML/WARC reference, language, content type, fetch status, fetch timestamps, content hash, outlinks, inlinks/anchor text when available, and metadata for snippets and Tavily `raw_content`. Without a document store, high-quality snippets, phrase search, Tavily-compatible `include_raw_content`, answer generation, and semantic reranking are not achievable.

Tavily API Agent: Owns Tavily-compatible `POST /search`. Minimum request fields are `query`, `search_depth`, `max_results`, `include_answer`, `include_raw_content`, `include_domains`, `exclude_domains`, `topic`, `time_range`, and `safe_search`. Response shape follows `query`, optional `answer`, `results[]` with `title`, `url`, `content`, optional `raw_content`, `score`, optional `published_date`, and `response_time`. Provider order is local full-text, optional local semantic/vector, yago/yacy peers, then an optional admin-toggled DDGS web-search provider. `Enabled on search miss` invokes DDGS only after exact/morphological local-plus-federated retrieval and the applicable bounded local recovery produce no result: local-exact rescue for an empty incomplete stage, or local fuzzy recovery after an honest miss. `Always` invokes DDGS in parallel with local and federated search. Internal provenance remains `ddgs`, human-facing search surfaces label those results `web`, and Tavily-compatible responses do not add a provider marker. Discovered web URLs may optionally be seeded to the crawler. There is no outbound upstream Tavily provider. Do not turn Tavily API into a simple proxy.

YaCy Search API Agent: Owns `/yacysearch.json`, `/yacysearch.rss`, OpenSearch-style compatibility, `resource=local|global`, query parameter translation, feasible filters by site/domain/filetype/protocol/language/date, and compatibility notes for unsupported YaCy-specific ranking/profile fields.

Crawler Agent: Owns a production-grade crawler with persistent frontier, states `queued`, `fetching`, `fetched`, `failed`, `deferred`, `blocked`, robots.txt, crawl-delay, per-host token bucket, sitemap ingestion, URL normalization, redirects, canonical link handling, content hash deduplication, HTTP fast fetch path, browser fallback only when needed, content extraction, MIME/size/time limits, retry policy, recrawl scheduling, backpressure from index/searchd, and the node's gRPC crawl transport integration if used.

Security Agent: Owns SSRF protection, denial of private CIDR/localhost/link-local/multicast/metadata IPs, DNS rebinding protection, crawl sandboxing, max body size, max redirects, allowed schemes, API auth, admin auth, remote crawl default-deny, peer quotas, and spam/index poisoning protections.

Admin UI Agent: Owns IBM Carbon admin UI. UI categories should be comparable to original YaCy administration without copying the legacy servlet UI: Search console, Crawl profiles, Crawl queue, P2P network, Peer details, Seedlists, DHT transfer in/out, Index/storage, Document browser, Search backend status, Tavily-compatible API settings, Security settings, Remote crawl policy, Metrics/performance, Logs/events, and Configuration. Use IBM Carbon with React via Next.js or Vite, and prefer a typed API client generated from OpenAPI when practical.

Observability Agent: Owns `/health`, `/ready`, Prometheus metrics, structured logs, tracing if practical, crawl queue depth, fetch rate, index rate, search latency, P2P transfer success/failure, peer reputation metrics, storage usage, and compaction stats.

QA / Compatibility Agent: Owns unit tests, integration tests, e2e tests with local multi-peer setup, race tests, golden tests for YaCy wire protocol, compatibility matrix against Java YaCy, crawler safety tests, search relevance fixtures, Tavily API contract tests, and UI smoke tests.

Roadmap priorities:

- P0: Align documentation status across README, FEATURES, crawler README, and plan files; add document store; add search backend abstraction; use an embedded pure-Go Bleve full-text backend tuned for web search; implement local Tavily-compatible `/search`; implement `/yacysearch.json` and `/yacysearch.rss` on the search core; make snippets come from document store, not only URL metadata; keep RWI generation and YaCy P2P compatibility working.
- P1 crawler: persistent frontier, robots/politeness, sitemap ingestion, canonicalization, deduplication, browser fallback, backpressure, SSRF protection, and recrawl scheduler.
- P1 P2P/DHT: outbound DHT scheduler, peer selection, batching, retry policy, redundancy, delete-after-success policy where safe, peer reputation, strict remote crawl policy, and compatibility status endpoint.
- P1 admin UI: Search console, Crawl management, P2P network, Index/storage, Security, Metrics, Logs/events, and Settings.
- P2: positional phrase/proximity search, domain quality/freshness ranking, optional Qdrant vector/semantic rerank, optional local answer generation from snippets, federated result fusion across peers, and native libp2p-based `yago-v2` network.

Strategic guardrails: Do not reimplement Java YaCy internals blindly. Do not introduce JVM, Solr, Lucene, or Kelondro as required runtime dependencies. Do not query any external web-search provider by default; the DDGS fallback is admin-opt-in only. Do not use RWI as the only local search index. Do not store raw crawled content without size limits and security policy. Do not allow remote crawl by default. Do not crawl private/local networks. Do not let UI drive unstable internal APIs directly; expose typed admin APIs. Do not break existing YaCy wire compatibility endpoints. Do not delete existing compatibility docs; update them with status notes instead.

Settings parity: Every environment variable that controls node or crawler behavior must have a matching setting in the admin console, editable at runtime, with the environment variable serving as its bootstrap default. Introducing or changing a controlling environment variable and exposing its admin setting happen in the same change — an env-only control is not complete. This keeps the admin console the single operator surface and the environment merely the initial default.

Continuity: Maintain one workspace ledger in `CONTINUITY.md`. At the start of every assistant turn, read it, update it with the current goal, constraints, decisions, progress state, and important tool outcomes, then continue the work. Keep it short: facts only, bullets preferred, uncertainty marked `UNCONFIRMED`. Keep `functions.update_plan` for short-term execution scaffolding and `CONTINUITY.md` for durable session state. Replies start with a brief Ledger Snapshot containing Goal, Now/Next, and Open Questions. The ledger keeps these headings: Goal (incl. success criteria), Constraints/Assumptions, Key decisions, State, Done, Now, Next, Open questions, Working set (files/ids/commands).

Жди EOF, а если ты его не видишь, значит файл неполный.

Feature catalog: Maintain `FEATURES.md` in the workspace root. It describes project capabilities side by side across `yagonode`, `yagocrawler`, `yagocrawlcontract`, `yagomodel`, and `yagoproto` where relevant. When adding a feature or changing behavior, update the affected capability, surface, status, behavior summary, and relevant files/tests.

Code structure: Follow OCP. Add features in new files and connect them through the smallest seam; do not grow existing files.

Module boundaries: Keep YaCy value types in `yagomodel`, wire protocol DTOs and endpoint vocabulary in `yagoproto`, node runtime/storage/P2P/search/ops behavior in `yagonode`, crawler worker behavior in `yagocrawler`, and node-crawler message contracts in `yagocrawlcontract`. Do not mix crawler runtime code into the node except through the contract and narrow node-side crawl orchestration.

Logging: Use stable message constants; put variable data in key/value fields. Happy paths: DEBUG. Sad paths: WARN if recoverable, ERROR if action is needed.

Comments: No comments without explicit user approval. Use naming and structure instead. Put required prose in `yagonode/doc/` or the relevant module README. Godoc package docs are allowed. If a comment seems unavoidable, ask first.

Single source of truth: Do not duplicate facts in comments, errors, logs, or similar text when they already exist in constants, config, docs, or protocol definitions.

Documentation: Each doc is self-contained, concise, plain-language, and user-facing. Links are for navigation only. Avoid cross-doc dependencies, duplicate facts, jargon, implementation details, and rationale. Behavior changes update the relevant module README, `yagonode/doc/`, `FEATURES.md`, and `CONTINUITY.md`; update a root README if one is introduced. Any change to the deployed surface — ports, listeners, service topology, images, or the environment variables a node or crawler reads — must also update `docker-compose.yml.example` (its `ports`, `services`, and `environment` blocks) and the `deploy/systemd/` unit and env-file examples in the same change; these are the canonical deployment references and are kept in sync, never left to drift.

Deployment targets: The `yago-node` and `yagocrawler` binaries must run identically under Docker, systemd on bare metal, and a Debian `.deb` install; see `deploy/README.md`. Do not hardcode a single deployment's assumptions into runtime code. Container-only choices — the bundled firefox-esr browser path, a disabled Firefox content sandbox, and the container filesystem layout — are not defaults baked into the binary; every one is selected through configuration/environment with a default that starts on all targets (a bare domain and its `www.` variant, `YAGOCRAWLER_BROWSER_PATH` empty for PATH discovery, `YAGOCRAWLER_BROWSER_SANDBOX` off) and an override for the target that differs. Runtime state lives under an operator-chosen directory (`YAGO_DATA_DIR`), never a fixed container path. Trust roots, the browser binary, and other host facilities come from the OS on bare metal (installed as package dependencies) and from image contents under Docker; the binary discovers them through the environment, it does not assume either. Any new deployment-specific behavior lands as configuration with a cross-target-safe default and is documented for Docker, systemd, and `.deb` together.

Release procedure: A release ships exactly what is on `main`, so land and feature-close every change first (tests, `make verify`, Dockerized scans, `FEATURES.md` and docs, commit, push). Then cut the release in strict order. First, tag the released `main` commit with a semantic-version `vX.Y.Z` tag and push the tag — that tag push is the only new-release trigger. The single CI workflow `.github/workflows/release.yml` fires on `v*` tags, runs `make verify`, then builds the `yago-node` and `yagocrawler` binaries for Linux `amd64` and `arm64` with the complete tag injected via ldflags (`GITHUB_REF_NAME`, including its leading `v`), while package metadata uses the numeric part, and publishes the `.deb`, `.rpm`, and tarball artifacts on the GitHub Release through `deploy/debian/build-deb.sh` and `deploy/rpm/build-rpm.sh`. A blocking native Linux `amd64`/`arm64` matrix also builds both product containers from the tagged source with exact version and source-revision identity, smoke-tests their architecture, labels, binaries, and bundled browser, and rejects HIGH or CRITICAL Trivy 0.72.0 vulnerability, secret, or misconfiguration findings. The native jobs export those exact validated images as short-lived workflow artifacts; a separate job verifies them again, publishes one Linux amd64/arm64 multi-architecture manifest list per product as `ghcr.io/d4rk4/yago-node:vX.Y.Z` and `ghcr.io/d4rk4/yagocrawler:vX.Y.Z`, and attaches and verifies GitHub-hosted provenance for each final manifest-list digest before the GitHub Release is published. The complete immutable semantic-version tag is the only operator-facing image tag; architecture-suffixed immutable staging references may exist, but never publish `latest`, major-only, minor-only, branch, or date aliases. The packages must be explicitly public and pass anonymous exact-version and digest pulls before the release is considered complete. The GitHub Release assets and GHCR packages are separate distribution surfaces.

A controlled existing-release event may backfill missing GHCR images for an immutable release. Its temporary path must be pinned to the exact release identity, semantic-version tag, tag ref, and expected source commit, verify main ancestry, check out that commit, repeat the native validation and registry publication path, and disable package construction and GitHub Release creation. It refuses to move the tag, recreate package assets or the GitHub Release, replace an existing release manifest, or create a mutable alias. The evidence records the current workflow-definition commit separately from the historical release source while preserving the exact tag ref and source digest in the attestation. Correct the historical release memo only after the public manifest digests, platforms, labels, versions, and attestations have been verified; identify the correction and its date explicitly, then remove the temporary event path.

Never hand-build or upload release artifacts locally; the tag is the source of truth and the tag and the injected build version stay identical. The build jobs create a signed GitHub artifact attestation for every release asset after package construction and the applicable amd64 smoke tests, and the release job verifies every downloaded attestation against the repository, exact release workflow, tag ref, and source commit before publication. Before tagging, commit the detailed human-authored release notes from the actual released diff and validation evidence as `doc/releases/vX.Y.Z.md`; the release workflow validates and publishes that exact file and refuses to create a release when it is missing or malformed. GitHub release notes use an old-school IBM/Sun/IETF engineering-memo style: precise plain English with no promotional language. Begin with `## Abstract`, containing a self-contained summary of at most 120 words that covers scope, operator impact, and compatibility or upgrade risk, immediately followed by the literal `<!--more-->` delimiter. After the delimiter, use the stable sections `## Release identity`, `## Changes`, `## Compatibility`, `## Upgrade and rollback`, `## Verification`, and `## Known limitations`. Derive every claim from the released tag-to-previous-tag diff and recorded validation evidence; never attribute later or unreleased work to an earlier release, and never substitute an autogenerated commit list for authored notes. Historical edits may add only clearly identified factual corrections. Deploy a node only from the published artifact for its architecture, never from a local build. On a generic bare-metal production node the upgrade order is: fetch the release `.deb`, verify its artifact attestation against repository `D4rk4/yago`, the exact release workflow, its tag ref, and its source commit, back up `/opt/yago/data`, stop `yago-node` and `yagocrawler`, install with `dpkg -i`, then manually start the services (the package postinst enables but does not start them), and verify both came up. Production package installs and service restarts are operator-authorized actions, never automatic.

Production target exception: deployments to `root@yagoseek.dev` do not create a new pre-upgrade backup. Preserve every existing archive under `/opt/yago/backups` unchanged, stage and verify the published package under `/opt/yago/releases`, stop both services, install it, then manually start and verify the node before the crawler. This target-specific downtime policy does not weaken the generic backup requirement or authorize deleting, replacing, or pruning an existing backup.

Naming: Every package, file, type, interface, port, function, method, field, and variable has one bounded responsibility. Prefer explicit bounded names over short generic names. Never use `util.go`, `helpers.go`, `handler.go`, or `types.go`. Reject umbrella names such as Store, Manager, Service, Handler, Util, or catch-all domain names like Distribution*. If the boundary cannot be stated in one sentence, fix the abstraction.

Naming style: Name a thing for what it is in the problem domain, never for how it is built or what it is for. Use the words a domain expert would say out loud; the same word holds in conversation, code, and docs. Strip implementation terms such as count, map, hash, digest, and buffer, and destination terms such as shared, peer, abstract, and response when they are not the domain noun. Spell names in full, readable English; length is free, abbreviation is not. Confine protocol- and transport-specific vocabulary to the edge that translates to and from it; inner code speaks plain domain language. Test every name by reading it aloud in a sentence to someone who knows the domain but not the code.

Dependencies: Record each new third-party dependency in its own ADR before use.

Version pinning: Pin all versions. Runtime deps: `go.mod`. Build/lint tools: Go tool directives in `go.mod` or the pinned tool lock. `make verify` uses only pinned tools, never PATH versions.

Security scanning: Feature closure uses pinned Dockerized scanners, never host PATH installs. Run Semgrep as `docker run --rm -v "${PWD}:/src:ro" -w /src semgrep/semgrep:1.168.0 semgrep scan --config auto --error`. Run Trivy after container images are built: first scan the source tree with `docker run --rm -v "${PWD}:/src:ro" -v trivy-cache:/root/.cache aquasec/trivy:0.72.0 fs --scanners vuln,secret,misconfig --exit-code 1 --severity HIGH,CRITICAL /src`, then scan each built project image with `docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v trivy-cache:/root/.cache aquasec/trivy:0.72.0 image --scanners vuln,secret,misconfig --image-config-scanners secret,misconfig --exit-code 1 --severity HIGH,CRITICAL <image>`.

Research: During planning for every task, do a short internet and arXiv research pass for relevant best practices, recent research, and implementation options, including but not limited to optimal architecture. Prefer primary sources such as specifications, official docs, source code, standards, and papers. If internet access is unavailable, record that in `CONTINUITY.md` and proceed from local context. Research does not create runtime internet dependencies.

Semantic behavior: Do not fix search, crawl routing, ranking, evidence selection, or compatibility behavior with ad hoc word lists, vendor facts, localized synonym buckets, or regexes that recognize specific meanings. Prefer protocol/data structures, parsed metadata, bounded model or ranking signals where such systems exist, and evidence from stored data. Regex is allowed for syntax-level parsing, protocol formats, numeric/unit tokenization, stable identifier normalization, security redaction, and file/URL/schema handling.

Testing: Code lands with tests, written in the same change as the code they exercise — never deferred to a follow-up. Pure documentation/configuration changes need lightweight validation only. For code changes, run focused tests first when useful, then `make verify`. Record exact commands and results in `CONTINUITY.md` and the final response. If a test cannot be added or run, state the concrete reason and residual risk.

UI validation: Always start a local instance for every UI change and verify the rendered result with screenshots in both Chrome or Chromium and Firefox. Automated markup or handler tests do not replace this visual cross-browser check.

Coverage: If coverage drops, first remove or refactor code. Find uncovered statements/branches and ask whether they should exist. Delete dead or defensive-only code, collapse unexercised branches, or replace several paths with one covered path. Add tests only for required behavior. Filler tests written only to raise coverage fail the change.

Commits: Every commit needs both a short subject and a body. The body must include `Motivation:`, `Summary:`, `What's new:`, and `Validation:` sections, and each heading must start on its own physical line. Use real line breaks in commit bodies; do not embed literal `\n` escape sequences. Do not pass one shell-quoted string containing `\n` to `git commit -m`, because Git records those characters literally. When creating commits from the shell, use repeated `-m` arguments, an editor, or a commit message file so formatting is preserved. Preferred shell form:

```sh
git commit -m "Subject in imperative mood" \
  -m "Motivation: Why this change is needed." \
  -m "Summary: What changed." \
  -m "What's new: User-visible or operational changes." \
  -m "Validation: Commands and results."
```

Before pushing, inspect the new commit with `git log -1 --format=%B`; if the body contains literal `\n` text or is missing `Motivation:`, amend it locally before push. Required pre-push checks:

```sh
git log -1 --format=%B | grep -F "Motivation:"
! git log -1 --format=%B | grep -F "\\n"
```

Keep commit text in English.

Feature closure: Closing a feature requires the tests to be written with the code, then a full test run, Dockerized Semgrep, Dockerized Trivy source and container-image scans, container builds, sanity tests, and smoke tests, and updating `FEATURES.md` (the affected capability, surface, status, behavior summary, and files/tests) plus `docker-compose.yml.example` when the deployed surface (ports, listeners, services, images, or env vars) changed, alongside the code — a slice is not closed until its `FEATURES.md` row and, where relevant, the compose example reflect it. If all feature-closure checks pass, commit the change and push it to `main`.

Gate: `make verify` is the mandatory code gate. A code change is not done until it is green. Feature closure adds the required Dockerized Semgrep, Dockerized Trivy, container, sanity, smoke, commit, and push checks.
