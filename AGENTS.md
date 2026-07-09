Lightweight Go senior YaCy node for DHT RWI storage and serving. Spec: `services/yacynode/doc/specification.md`.

Code structure: Follow OCP: add features in new files and connect them through the smallest seam; do not grow existing files.

Logging: Use stable message constants; put variable data in key/value fields. Happy paths: DEBUG. Sad paths: WARN if recoverable, ERROR if action is needed.

Comments: No comments without explicit user approval. Use naming and structure instead. Put required prose in services/yacynode/doc/. Godoc package docs are allowed. If a comment seems unavoidable, ask first.

Single source of truth: Do not duplicate facts in comments, errors, logs, or similar text when they already exist in constants, config, or docs.

Documentation: Each doc is self-contained, concise, plain-language, and user-facing. Links are for navigation only. Avoid cross-doc dependencies, duplicate facts, jargon, implementation details, and rationale.

Naming: Every package, file, type, interface, port, function, method, field, and variable has one bounded responsibility. Prefer explicit bounded names over short generic ones. Never use util.go, helpers.go, handler.go, or types.go. Reject umbrella names such as Store, Manager, Service, Handler, Util, or catch-all domain names like Distribution*. If the boundary cannot be stated in one sentence, fix the abstraction.

Naming symmetry: When one variant of a thing is qualified, qualify every sibling the same way. Parallel implementations get parallel names (elasticsearchSearchOnce, manticoreSearchOnce); never leave one sibling bare.

Naming style: Name a thing for what it is in the problem domain, never for how it is built or what it is for. Use the words a domain expert would say out loud; the same word holds in conversation, code, and docs. Strip implementation terms (count, map, hash, digest, buffer) and destination terms (shared, peer, abstract, response) from names; keep the domain noun. Spell names in full, readable English; length is free, abbreviation is not. Confine protocol- and transport-specific vocabulary to the edge that translates to and from it; inner code speaks plain domain language. Test every name by reading it aloud in a sentence to someone who knows the domain but not the code: if they nod, it passes.

Dependencies: Record each new third-party dependency in its own ADR before use.

Version pinning: Pin all versions. Runtime deps: go.mod. Build/lint tools: Go tool directives in go.mod. make verify uses only pinned tools, never PATH versions.

Testing: Code lands with tests. make verify runs tests and coverage. A change is done only when both pass and total coverage stays at or above the configured threshold.

Coverage: If coverage drops, first remove or refactor code. Find uncovered statements/branches and ask whether they should exist. Delete dead or defensive-only code, collapse unexercised branches, or replace several paths with one covered path. Add tests only for required behavior. Filler tests written only to raise coverage fail the change.

Gate: make verify is the only gate. A change is not done until it is green.
