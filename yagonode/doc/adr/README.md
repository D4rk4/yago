# Architecture Decision Records

This directory holds the architecture decision records (ADRs) for `yago`,
following the format described in
[ADR-0001](0001-record-architecture-decisions.md). Copy
[0000-template.md](0000-template.md) to start a new record and give it the next
number.

## Dependency Rule

Every new third-party runtime dependency is recorded in its own ADR before it is
used. The record names the module, its pinned version, its license, and the
alternatives considered. All versions are pinned: runtime dependencies in
`go.mod`, and build or lint tools through the pinned toolchain flow, so
`make verify` uses only pinned tools and never a version from `PATH`.

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0000](0000-template.md) | Template for new records | Template |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-layered-architecture.md) | Separate HTTP handlers, domain logic, and adapters | Accepted |
| [0003](0003-automated-quality-gate.md) | Enforce quality automatically through `make verify` | Accepted |
| [0004](0004-isolate-wire-protocol-module.md) | Keep the YaCy models and protocol in standalone, reusable modules | Accepted |
| [0005](0005-use-bbolt-for-embedded-storage.md) | Use bbolt for embedded storage | Accepted |
| [0006](0006-use-testcontainers-for-e2e-tests.md) | Use testcontainers-go for end-to-end tests | Accepted |
| [0007](0007-use-nats-jetstream-for-node-crawler-queue.md) | Use NATS with JetStream for the node↔crawler message queue | Superseded by [ADR-0014](0014-grpc-node-crawler-transport.md) |
| [0008](0008-evict-rwi-postings-under-quota-pressure.md) | Evict RWI postings under quota pressure | Accepted |
| [0009](0009-modular-vertical-slices.md) | Organize features as vertical slices over a storage kernel | Accepted |
| [0010](0010-boltvault-storage-kernel.md) | Own the embedded database behind a storage kernel | Accepted |
| [0011](0011-use-prometheus-client-for-metrics.md) | Expose node metrics through the Prometheus client | Accepted |
| [0012](0012-use-bleve-for-embedded-full-text-fallback.md) | Use Bleve for the embedded full-text fallback | Accepted |
| [0013](0013-in-process-egress-guard.md) | Screen outbound connections with an in-process dial-time guard | Accepted |
| [0014](0014-grpc-node-crawler-transport.md) | Carry node↔crawler traffic over gRPC with a node-hosted queue | Accepted |
| [0015](0015-argon2id-admin-password-hashing.md) | Hash admin passwords with Argon2id | Accepted |
| [0016](0016-crawler-prometheus-metrics.md) | Expose crawler metrics through the Prometheus client | Accepted |
| [0017](0017-crawl-order-lease-delivery.md) | Deliver crawl orders through durable leases with worker heartbeats | Accepted |
| [0018](0018-commit-to-bleve-web-search-backend.md) | Commit to Bleve as the web-search backend | Accepted |
| [0019](0019-ddgs-web-search-fallback.md) | Optional DDGS web-search fallback instead of an upstream Tavily provider | Accepted |
| [0020](0020-public-search-portal.md) | Public search portal as a progressively-enhanced surface separate from the admin SPA | Accepted |
| [0021](0021-in-house-metasearch-backend.md) | In-house multi-engine metasearch backend for the DDGS fallback | Accepted |
| [0022](0022-server-rendered-admin-console.md) | Server-rendered admin console with htmx instead of a React SPA | Accepted |
| [0023](0023-rename-go-modules-yacy-to-yago.md) | Rename the Go modules and import paths from yacy* to yago* | Accepted |
| [0024](0024-local-host-block-rank-signal.md) | Local host block-rank ranking signal | Accepted |
| [0025](0025-sharded-compressed-vault-and-index.md) | Shard the vault and search index into bounded compressed files | Accepted |
| [0026](0026-per-language-morphology-and-analyzer-routing.md) | Route documents to per-language analyzers for multilingual morphology | Accepted |
| [0027](0027-swarm-morphological-query-expansion.md) | Expand swarm queries into bounded analyzer-consistent inflections | Accepted |
| [0028](0028-local-morphology-followups-disposition.md) | Close the local-morphology and partial-word follow-ups as subsumed | Accepted |
| [0029](0029-document-expansion-anchor-text-vs-doc2query.md) | Deliver document expansion via inbound anchor text; reject model-based doc2query | Accepted |
| [0030](0030-optional-cpu-dense-retrieval-side.md) | Reject a CPU dense retrieval side for the current architecture | Superseded by [ADR-0048](0048-bounded-google-yandex-ranking-research-disposition.md) |
| [0031](0031-learned-sparse-rwi-no-go.md) | Do not move the RWI to learned-sparse (SPLADE) weights | Accepted |
| [0032](0032-private-query-search-no-go.md) | Do not adopt Tiptoe-style private search; keep it behind the dense side | Accepted |
| [0033](0033-operator-editable-public-portal-handlebars.md) | Operator-editable public portal via server-side Handlebars with a GrapesJS admin editor | Accepted |
| [0034](0034-dead-page-removal-on-recrawl.md) | Remove a dead page from the index when a recrawl finds it permanently gone (404/410) | Accepted |
| [0035](0035-learned-log-linear-ranking-yagorank.md) | Learned log-linear ranking (YagoRank): fit ranking weights to NDCG, add quality + unordered-SDM features | Superseded by [ADR-0043](0043-pure-go-evidence-learning-to-rank.md) |
| [0036](0036-storage-usage-accounting-document-eviction-and-compaction.md) | Truthful storage accounting (live bytes), evict documents on purge, and periodic configurable compaction | Accepted |
| [0037](0037-dynamic-shard-growth-linear-hashing.md) | Grow the shard pool dynamically by linear hashing as data accumulates; make the storage quota a live soft admission target | Accepted |
| [0038](0038-bounded-loss-deferred-fsync.md) | Opt-in, restart-required deferred fsync (bbolt NoSync) with a staggered background flush that bounds the loss window | Accepted |
| [0039](0039-binary-fuse-rwi-shard-filters.md) | Per-shard binary-fuse term filters skip RWI shards that provably lack a query term (adopts the xorfilter dependency) | Accepted |
| [0040](0040-rwi-topk-pruning-not-maxscore.md) | Prune RWI top-k with bounded selection and reporting-gated scan early termination instead of the architecturally inapplicable MaxScore | Accepted |
| [0041](0041-perf-io-scorch-reclaim-crawler-cgroup-no-bbolt-madvise.md) | PERF-IO-02: raise scorch delete-reclaim weight and bound the crawler with cgroup limits; do not add raw bbolt mmap madvise | Accepted |
| [0042](0042-content-type-authoritative-filetype-classification.md) | Classify file types from authoritative content type with URL suffix fallback | Accepted |
| [0043](0043-pure-go-evidence-learning-to-rank.md) | Pure-Go evidence learning-to-rank with held-out promotion | Accepted |
| [0044](0044-alpine-browser-crawler-runtime.md) | Alpine browser crawler runtime | Accepted |
| [0045](0045-digest-pinned-image-provenance.md) | Pin container bases by digest and label source provenance | Accepted |
| [0046](0046-vendor-font-awesome-for-grapesjs-icons.md) | Vendor Font Awesome for GrapesJS legacy controls | Accepted |
| [0047](0047-morphology-and-positional-ranking-evidence.md) | Separate morphology recall from positional ranking evidence | Accepted |
| [0048](0048-bounded-google-yandex-ranking-research-disposition.md) | Adopt only bounded and independently justified ranking evidence | Accepted |
| [0049](0049-vendor-haiku-icons-for-admin-shelf.md) | Vendor Haiku icons for the Admin shelf | Accepted |
| [0050](0050-attest-release-artifacts.md) | Attest release artifacts with GitHub-hosted provenance | Accepted |
| [0051](0051-promote-validated-release-images-to-ghcr.md) | Promote validated release images to attested GHCR manifest lists | Accepted |
| [0052](0052-persist-crawler-frontier-checkpoints-with-bbolt.md) | Persist exact crawler frontier checkpoints with bbolt | Accepted |
| [0053](0053-use-vellum-for-compact-cjk-lexicons.md) | Use vellum for compact CJK lexicons | Accepted |
| [0054](0054-derive-chinese-segmentation-from-jieba.md) | Derive Chinese segmentation from the Jieba dictionary | Accepted |
| [0055](0055-derive-japanese-segmentation-from-sudachidict-small.md) | Derive Japanese segmentation from SudachiDict Small | Accepted |
| [0056](0056-derive-equal-width-chinese-conversion-from-opencc.md) | Derive equal-width Chinese conversion from OpenCC | Accepted |
| [0057](0057-negotiate-cross-node-query-match-evidence.md) | Negotiate bounded cross-node query-match evidence | Accepted |
| [0058](0058-use-negotiated-analyzer-recall-for-yago-peers.md) | Use negotiated analyzer recall for cooperating Yago peers | Accepted |
| [0059](0059-use-snowball-rules-for-bounded-surface-generation.md) | Use Snowball rules for bounded surface generation | Accepted |
| [0060](0060-admit-managed-growth-by-filesystem-reserve.md) | Admit managed growth by filesystem reserve | Accepted |
| [0061](0061-record-collection-length-on-record-shards.md) | Record collection length changes on record shards | Accepted |
