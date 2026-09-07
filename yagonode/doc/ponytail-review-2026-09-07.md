# Subsystem complexity review

Date: 2026-09-07. Scope: the workspace candidate based on `8a8c921c`, including the ADR-0084 implementation. This report records recommendations; the deletions below have not been applied.

The review indexed declarations, imports, symbol references and duplicate function bodies in 1,764 tracked non-test Go source files across six modules. Every package was assigned to one of the 23 groups below. Candidate deletions were checked against production callers, tests, templates and current assembly. Admin JavaScript, dependency declarations, build tooling and deployment entry points were also inspected. Generated protobuf, vendored libraries and dictionary data were inventoried rather than reviewed line by line. This is a complexity review, not a claim that every execution path is defect-free.

The main finding is retained predecessor code exercised only by tests. Removing it should move useful assertions onto the implementation the products actually call. Tests for ownership, durable writes, bounded work and security remain necessary.

Source locations below are relative to the `yagonode` module; `../yago-crawler` names the sibling crawler module.

## Ranked cuts

The line figures count existing source selected for removal. They exclude test changes and do not predict executable size or latency.

| ID | Location | Finding and replacement | Existing lines |
| --- | --- | --- | ---: |
| R01 | `internal/searcheval/platt_calibration.go:46`; `internal/searcheval/isotonic_calibration.go:22` | **yagni:** Both calibration implementations and their serialization are consumed only by their own tests. Remove the unused algorithms until a training or evaluation workflow needs them. Retain the active ranking evaluation and content-safety calibration. | 436 |
| R02 | `internal/crawlresults/content_clustering.go:230`; `content_cluster_deletion.go:15`; `content_cluster_deletion_projection.go:12` | **delete:** The ingest-side cluster-deletion chain has no production caller. Remove it and its exclusive projection functions; current deletion runs through `documentLineageEvictor.Delete`. Retain projection functions used by ingestion. | 181 |
| R03 | `internal/hostrank/hostrank.go:33` | **delete:** The predecessor `Compute`, `citationGraph` and its propagation/normalization are test-only. Remove them; the node calls `ComputeDomainAuthority`. Keep the authority value types and constants used by that implementation. | 93 |
| R04 | `internal/crawlbroker/worker_lease_replay.go:15`; `order_lease.go:69` | **delete:** Worker-wide replay plus `leaseNext`, `leasePop` and `ackLease` remain only for tests. Exercise current session-aware leasing and acknowledgment instead. Keep `ackLeaseWithTarget`, which still serves node-owned completion. | 85 |
| R05 | `../yago-crawler/internal/crawlorder/grpc_order_receiver.go:160`; `lease_settlement_retry.go:43` | **delete:** Sessionless stream/drain/settlement adapters and the extra drain delegate have no independent product use. Call the session-aware delivery and settlement paths directly; adapt tests to those paths. | 75 |
| R06 | `../yago-crawler/internal/frontiercheckpoint/redirect_and_host_state.go:11` | **delete:** `RecordHostState` has no production caller. Remove this predecessor entry point; `CompletePage` owns the live host-progress update. Keep the shared host validation and chunked retirement functions. | 73 |
| R07 | `internal/searchremote/peer_search_transport.go:14`; `searchremote.go:431` | **delete:** `remoteSearch`, its exclusive send wrapper, `readRemoteSearchResponse`, `queryPeerJobs`, `termAbstracts` and `searchResults` preserve test-only entry points. Test the current bounded transport and query-budget paths directly. | 58 |
| R08 | `../yago-crawler/internal/formatparse/pdf.go:55`; `pdf_page_descriptions.go:83`; `pdf_simple_font_encoding.go:12`; `pdf_tounicode.go:31` | **delete:** Twelve old parser convenience functions are called only by tests. Remove them after switching those tests to the existing quota-aware decoders and text collector. Keep PDF parsing and all extraction limits. | 57 |
| R09 | `../yago-crawler/internal/firefoxfetch/browser_page_fetcher.go:62`; `firefox_pool.go:26`; `browser_slot_acquisition_deadline.go:3` | **delete:** The legacy deadline callback constructors remain beside the active `BrowserPoolObserver` path. Remove the old constructors, callback selector and their exclusive observation plumbing; use the current observer in tests. | 35 |
| R10 | `internal/adminauth/console.go:47`; `api_key_store.go:195` | **delete:** `KnownScopes`, `ListAPIKeys` and the exclusive compatibility listing wrapper have no product caller. Keep `ListAPIKeyPage` and the scope parser; migrate useful listing assertions to pagination. | 28 |
| R11 | `internal/tavilyapi/markdown_render.go:16` | **delete:** The unbounded `documentMarkdown` renderer is test-only. Use `boundedDocumentMarkdown` in its tests and retain `relevantChunks`, which still serves requests. | 27 |
| R12 | `internal/searchindex/cached_result_clone.go:130`; `internal/searchsession/session_payload_clone.go:52,115` | **stdlib:** Replace the three scalar slice-copy functions with `slices.Clone` at their callers. Preserve deep copies of nested collections and `strings.Clone`, which releases retained backing storage. | 27 |
| R13 | `internal/adminui/admin_asset_reference.go:44` | **delete:** `mustAdminAssetReferences`, `buildAdminAssetReferences` and their exclusive map projection duplicate the current asset-catalog entry points for tests. Assert against the catalog directly. Keep revision and canonical-path checks. | 21 |
| R14 | `internal/searchcore/lexical_dependence.go:30` | **delete:** Three test-only functions discard one half of the current positional/text evidence result. Call the evidence functions directly and assert both returned signals where relevant. | 18 |
| R15 | `internal/documentsearch/search_request_translation.go:128`; `internal/yacysearch/json_request.go:82` | **stdlib:** Replace duplicate `firstNonEmpty` functions with `cmp.Or`. | 18 |
| R16 | `internal/dhtexchange/outbound_distributor.go:53,75` | **delete:** The non-confirming constructor and unrestricted `Distribute` entry point are test-only. Use `NewConfirmingOutboundDistributor` and `DistributeReady`; retain confirmation, retry and restoration logic. | 14 |
| R17 | `internal/yagonode/node_settings_catalog_crawler_policy.go:404` | **shrink:** `normalizeCrawlerRuntimeInteger` is identical to `normalizeBoundedInteger` in the same package. Use the existing function with the same bounds. | 8 |

Three additional cuts need a coordinated call-site change; they are excluded from the estimate:

- `internal/tavilyapi/search_endpoint.go:1051`: **delete:** `writeError` accepts and discards an error code and request ID. Remove those arguments and the exclusive plumbing from its eight caller files. Keep the detail-only envelope, status codes and successful-response request IDs.
- `../yago-crawler/internal/frontier/checkpoint_run_recovery.go:44`: **yagni:** The only production checkpoint supports bounded recovery, but the frontier maintains an optional interface and a full-snapshot fallback. Require bounded recovery at the persistent-checkpoint boundary; retain in-memory frontier operation, checkpoint validation and restart tests.
- `internal/crawlbroker/order_lease.go:62`; `internal/adminauth/session.go:28`; `internal/tracectx/tracectx.go:90`; `internal/tavilyapi/search_endpoint.go:1035`: **stdlib:** Remove impossible error paths and artificial failure injection around direct `crypto/rand.Read` calls. Keep token formats, entropy sizes, hashing and real storage errors. This does not apply to APIs accepting an arbitrary `io.Reader`.

The current ADR-0084 diff also repeats `providerEligible` through `shouldFallback` after `Search` has already established eligibility (`internal/websearch/fallback_searcher.go:66,87`). **shrink:** use the candidate threshold directly at that branch and test behavior through `Search`.

## Result by subsystem

“Keep” means no additional justified cut was established in this pass. It is not a blanket correctness approval.

| Subsystem | Go files | Result |
| --- | ---: | --- |
| Node assembly and settings | 286 | R17. Keep explicit assembly, runtime toggles and the single-current-pipeline web mode switch; no new runtime service is needed. |
| Local retrieval and query analysis | 95 | R14–R15. Keep document-backed retrieval, exact/fuzzy recovery, positional evidence, filters and the RWI compatibility boundary. |
| Full-text backend and cache | 101 | R12. Keep the `SearchIndex` boundary, Bleve disk/memory implementations, cache budgets and index-generation migration. The large CJK table is data. |
| Federated retrieval and search sessions | 41 | R07, R12. Keep per-request work budgets, cancellation, retained completed pages and stable session paging. |
| Web supplementation | 29 | Remove the repeated eligibility decision. Keep shared fusion/completion and the primary-result snapshot needed to reject duplicate-only engine batches while requests overlap. |
| Ranking, evaluation and content signals | 86 | R01, R03. Keep the active learned ranking, evaluation gates, domain authority and content-safety models. |
| Document and index storage | 152 | Keep transaction ownership, lineage reservations, anchor finalization, tombstones, sharding and recovery. The memory vault and vault test support are used by tests; absence of a product importer alone does not justify deleting them. |
| Node crawl orchestration and transport | 156 | R02, R04. Keep session ownership, queue admission, durable settlement and storage backpressure. |
| P2P membership and compatibility | 118 | Keep seed parsing, liveness, reputation, peer news, remote-crawl admission and protocol adapters. Small duplicated transport-close functions do not justify a new common package. |
| DHT transfer | 27 | R16. Keep target selection, gates, bounded batches, retry and confirmed local completion. |
| Public APIs and portal | 94 | R11, R15 and the discarded `writeError` arguments. Keep separate Tavily/YaCy translation and bounded raw-content assembly. |
| Authentication and HTTP security | 58 | R10 and the direct-randomness error cleanup. Keep CSRF, authentication, scopes, route validation, body admission and rate limits. |
| Administration UI | 64 | R13. Keep server-rendered forms and accessibility behavior. Template-invoked `PeerTableView` methods are live despite having no Go call sites. |
| Observability | 26 | Keep typed metrics, exemplars, bounded history and durable events. The trace randomness fallback is covered by the separate stdlib finding. |
| Crawler process and policy | 40 | Keep explicit process supervision, live policy application and node transport. Startup test convenience wrappers can be folded into tests when their files next change. |
| Crawler orders, leases and delivery | 71 | R05. Keep the terminal outbox and separate progress delivery: terminal outcomes survive restarts; progress can be coalesced. |
| Crawler frontier and checkpoint | 95 | R06 and the optional bounded-checkpoint cleanup. Keep persistent deduplication, due times, host fairness, cancellation and chunked recovery. |
| Crawler fetching and origin policy | 43 | R09. Keep HTTP-first fetch, bounded Firefox sessions, robots, politeness, sitemaps, fetch permits and browser trust validation. |
| Crawler parsing and indexing | 66 | R08. Keep bounded format decoders and extraction. A new parser service or dependency is not a smaller supported change. |
| YaCy value model | 45 | Keep stable hashes, seed values and encoding semantics; exported compatibility APIs are not dead merely because this node does not call every one. |
| YaCy wire protocol | 30 | Keep endpoint DTOs, field codecs, evidence validation and protocol fixtures. |
| Node-crawler contracts | 38 | Keep separate messages, validation, runtime policy and canonical URL identity. Do not move worker runtime into this module. |
| Shared network egress | 3 | Keep public-address admission and validation at dial time; URL prechecks and dial checks cover different points in resolution. |
| Deployment and release tooling | — | Keep the two-product topology, native architecture checks, immutable tags, attestation and anonymous pulls. They satisfy explicit release requirements. No deletion is proposed. |
| Frontend assets and dependencies | — | Keep htmx, Handlebars theme compatibility and the existing visual editor. Editor assets are loaded by the design page. No removable third-party dependency was confirmed. |

## Current candidate and validation

The candidate already corrects coalesced queue wakeups with generation broadcasts, removes the unused worker heartbeat/cache, shares crawl retry jitter, and makes the memory index close idempotently. Those changes are separate from the recommendations above. The web implementation retains one shared completion path and enables the privacy mode setting for new requests without restarting the node.

`env -u GOROOT GOTOOLCHAIN=go1.26.6 make verify` passed all analyzers, architecture and API checks, race tests, deterministic shuffle, fuzz smoke, exact coverage and builds. All six modules reached 100% of gated statements. Pinned Dockerized Semgrep and Trivy source/image scans passed. These results validate the current candidate; they do not validate the proposed deletions. Each removal needs focused tests of its retained path, followed by the full gate.

`env -u GOROOT GOTOOLCHAIN=go1.26.6 make e2e` passed: node/Java YaCy integration in 333.100 seconds and crawler integration in 115.239 seconds, including independent node and crawler restarts with continuing work. Chromium and Firefox rendered the updated web mode control correctly.

The stdlib recommendations were checked against [slices.Clone](https://pkg.go.dev/slices#Clone), [cmp.Or](https://pkg.go.dev/cmp#Or), and the documented guarantee that [crypto/rand.Read fills its input and does not return an error](https://pkg.go.dev/crypto/rand#Read). The [multi-stage retrieval research](https://arxiv.org/abs/1704.03970) informed the budget review; it does not establish a need for extra services or abstractions here.

The 17 quantified cuts select 1,254 existing source lines. Allowing for direct-call and import changes gives an estimated reduction of about 1,200 non-test source lines; test rewrites are not included.

net: approximately -1,200 source lines, -0 dependencies possible.
