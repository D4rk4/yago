# 3. Enforce quality automatically through `make verify`

Date: 2026-06-17

## Status

Accepted

## Context

The architecture (ADR 0002) and the conventions in `AGENTS.md` only hold if they are checked
mechanically; rules kept by reviewer discipline drift as the codebase grows. We want one gate
that fails fast on boundary violations, formatting drift, lint findings, and incomplete test
coverage, and that behaves identically on every machine.

## Decision

`make verify` is the single gate. It runs across `yagonode`, `yagocrawler`,
`yagocrawlcontract`, `yagomodel`, `yagoproto`, and `yagoegress`: a non-mutating
`go mod tidy -diff` check, formatting, `go vet`, lint, an architecture-boundary
check, race-enabled tests, exact coverage, and a build. A change is not done
until it is green.

- **Boundaries** are checked by `go-arch-lint` (`.go-arch-lint.yml`, version 3), which declares
  the `api`, `core`, `infrastructure`, and `cmd` components and the allowed dependencies
  between them.
- **Formatting and lint** use `golangci-lint` v2 (`.golangci.yml`). Its formatters block enables
  `gofumpt` (stricter gofmt), `gci` (deterministic import grouping: standard, default, local
  module), and `golines` (bounded line length). `make fmt` rewrites files; `make verify` runs
  `golangci-lint fmt --diff` to fail on any unformatted file. `.editorconfig` covers non-Go
  files.
- **Coverage** is gated from the raw Go cover profile. The checker sums integer statement
  weights and compares covered and total statements without formatting the percentage first.
  At the default `COVERAGE_MIN=100`, every statement in each module's gated production profile
  must be covered; a value such as 99.951% cannot pass because a display tool rounds it to
  100.0%. `make verify` first runs a checker self-test with exact, rounded, fractional-threshold,
  and empty profiles.

Both tools are pinned with platform checksums in `tools/tools.lock`, so `make verify` runs the
recorded versions from `.toolchain/bin` regardless of `PATH`, per the version-pinning rule in
`AGENTS.md`.

The module check runs the Go toolchain declared by the workspace and fails when
either `go.mod` or `go.sum` is not the deterministic result of `go mod tidy`.
`make tidy` remains the explicit mutating repair command; verification never
rewrites the worktree.

## Consequences

`go-arch-lint` v1.15.0 and `golangci-lint` v2.12.2 become build-time dependencies; this ADR is
their dependency record. Builds need the toolchain to fetch and compile them. In exchange the
architecture and conventions are enforced uniformly, rounded coverage cannot hide an uncovered
statement, and violations surface at `make verify` time rather than in review or production.
