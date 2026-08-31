# 3. Enforce quality automatically through `make verify`

Date: 2026-06-17

## Status

Accepted

Amended by [ADR-0079](0079-require-standalone-security-and-race-gates.md),
[ADR-0080](0080-ratchet-first-party-nil-flow-findings.md),
[ADR-0081](0081-protect-reusable-module-api-compatibility.md),
[ADR-0082](0082-reject-reachable-go-vulnerabilities.md), and
[ADR-0083](0083-shuffle-tests-and-smoke-fuzz-targets.md).

## Context

The architecture (ADR 0002) and the conventions in `AGENTS.md` only hold if they are checked
mechanically; rules kept by reviewer discipline drift as the codebase grows. We want one gate
that fails fast on boundary violations, formatting drift, lint findings, and incomplete test
coverage, and that behaves identically on every machine.

## Decision

`make verify` is the single gate. It runs across `yagonode`, `yago-crawler`,
`yagocrawlcontract`, `yagomodel`, `yagoproto`, and `yagoegress`: a non-mutating
`go mod tidy -diff` check, formatting, `go vet`, lint, an architecture-boundary
check, standalone gosec analysis, race-enabled tests, exact coverage, and a
build. It also ratchets NilAway findings, rejects incompatible reusable-module
API changes and reachable Go vulnerabilities, reruns tests in one recorded
shuffled order, and actively fuzzes every discovered fuzz target under a short
budget. A change is not done until the complete gate is green.

- **Boundaries** are checked by `go-arch-lint` (`.go-arch-lint.yml`, version 3), which declares
  the `api`, `core`, `infrastructure`, and `cmd` components and the allowed dependencies
  between them.
- **Formatting and lint** use `golangci-lint` v2 (`.golangci.yml`). Its formatters block enables
  `gofumpt` (stricter gofmt), `gci` (deterministic import grouping: standard, default, local
  module), and `golines` (bounded line length). `make fmt` rewrites files; `make verify` runs
  `golangci-lint fmt --diff` to fail on any unformatted file. `.editorconfig` covers non-Go
  files.
- **Static security** uses standalone gosec v2.28.0 across every production
  module. Generated files are excluded. G401 and G501 are excluded only for the
  legacy YaCy MD5 wire-hash implementation and remain active elsewhere.
- **Data races** are checked by the explicit `race` target, which runs every
  module test with `go test -race`; exact coverage repeats its workload with
  race instrumentation.
- **Nil flow** uses the pinned NilAway analyzer on first-party production code.
  Its exact current diagnostic locations form a ratchet: a new, removed, moved,
  or unclassified finding blocks until the baseline change is reviewed.
- **Public API compatibility** compares `yagomodel`, `yagoproto`,
  `yagocrawlcontract`, and `yagoegress` with the exact tag and commit of the
  previous published release. Failed release-attempt tags are not baselines.
- **Known Go vulnerabilities** use symbol-level govulncheck analysis across all
  production modules and fail for a reachable vulnerability or analysis error.
- **Order and input variation** reruns every module test with one positive
  tracked shuffle seed and no result cache, then actively fuzzes every
  discovered fuzz target for five seconds with one worker.
- **Coverage** is gated from the raw Go cover profile. The checker sums integer statement
  weights and compares covered and total statements without formatting the percentage first.
  At the default `COVERAGE_MIN=100`, every statement in each module's gated production profile
  must be covered; a value such as 99.951% cannot pass because a display tool rounds it to
  100.0%. `make verify` first runs a checker self-test with exact, rounded, fractional-threshold,
  and empty profiles.

Archive-distributed tools are pinned with platform checksums in
`tools/tools.lock`. Go-distributed quality tools are pinned by exact module
version in `tools/go-quality-tools.lock`, installed outside every product module
graph, and checked against their embedded module identity. `make verify` runs
the recorded binaries from `.toolchain/bin` regardless of `PATH`.

The module check runs the Go toolchain declared by the workspace and fails when
either `go.mod` or `go.sum` is not the deterministic result of `go mod tidy`.
`make tidy` remains the explicit mutating repair command; verification never
rewrites the worktree.

## Consequences

`go-arch-lint` v1.15.0, `golangci-lint` v2.12.2, gosec v2.28.0,
NilAway, apidiff, and govulncheck are build-time dependencies. ADR-0079 through
ADR-0082 are their dependency records. In exchange, architecture, formatting,
static security, nil-flow growth, reusable API compatibility, reachable Go
vulnerabilities, test-order dependence, active fuzz targets, exercised-path
races, and exact coverage are visible release blockers. Each analyzer remains
an approximation and does not replace review, integration tests, lifecycle
tests, Semgrep, Trivy, or operational observation.
