Lightweight Go senior YaCy node for DHT RWI storage and serving. Spec: `yacynode/doc/specification.md`.

Project language: Use English in code, documentation, generated project files, commits, and user-visible text. Exceptions are exact upstream protocol fixtures, exact legal license text, and externally supplied data that must remain verbatim.

Workspace instructions: This root `AGENTS.md` is the single canonical agent instruction file for the workspace. Do not add nested instruction files or tool-specific mirrors.

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

Research: For non-trivial architecture, protocol, dependency, security, search, crawling, ranking, or storage decisions, do a short research pass using primary sources such as specifications, official docs, source code, and papers. If internet access is unavailable, record that in `CONTINUITY.md` and proceed from local context. Research does not create runtime internet dependencies.

Semantic behavior: Do not fix search, crawl routing, ranking, evidence selection, or compatibility behavior with ad hoc word lists, vendor facts, localized synonym buckets, or regexes that recognize specific meanings. Prefer protocol/data structures, parsed metadata, bounded model or ranking signals where such systems exist, and evidence from stored data. Regex is allowed for syntax-level parsing, protocol formats, numeric/unit tokenization, stable identifier normalization, security redaction, and file/URL/schema handling.

Testing: Code lands with tests. Pure documentation/configuration changes need lightweight validation only. For code changes, run focused tests first when useful, then `make verify`. Record exact commands and results in `CONTINUITY.md` and the final response. If a test cannot be added or run, state the concrete reason and residual risk.

Coverage: If coverage drops, first remove or refactor code. Find uncovered statements/branches and ask whether they should exist. Delete dead or defensive-only code, collapse unexercised branches, or replace several paths with one covered path. Add tests only for required behavior. Filler tests written only to raise coverage fail the change.

Gate: `make verify` is the only gate. A code change is not done until it is green.
