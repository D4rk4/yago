# 3. Enforce quality automatically through `make verify`

Date: 2026-06-17

## Status

Accepted

## Context

The architecture (ADR 0002) and the conventions in `AGENTS.md` only hold if they are checked
mechanically; rules kept by reviewer discipline drift as the codebase grows. We want one gate
that fails fast on boundary violations, formatting drift, lint findings, and falling test
coverage, and that behaves identically on every machine.

## Decision

`make verify` is the single gate. It runs, across both workspace modules: a formatting check,
`go vet`, lint, an architecture-boundary check, the test suite with a coverage floor, and a
build. A change is not done until it is green.

- **Boundaries** are checked by `go-arch-lint` (`.go-arch-lint.yml`, version 3), which declares
  the `api`, `core`, `infrastructure`, and `cmd` components and the allowed dependencies
  between them.
- **Formatting and lint** use `golangci-lint` v2 (`.golangci.yml`). Its formatters block enables
  `gofumpt` (stricter gofmt), `gci` (deterministic import grouping: standard, default, local
  module), and `golines` (bounded line length). `make fmt` rewrites files; `make verify` runs
  `golangci-lint fmt --diff` to fail on any unformatted file. `.editorconfig` covers non-Go
  files.
- **Coverage** is gated by a check over `go tool cover -func`: `make verify` fails when total
  coverage in either module drops below `COVERAGE_MIN`. The threshold starts modest, because
  scaffolding has little testable code, and rises as features land.

Both tools are pinned as Go `tool` directives in the node `go.mod`, so `make verify` runs the
recorded versions regardless of `PATH`, per the version-pinning rule in `AGENTS.md`.

## Consequences

`go-arch-lint` v1.15.0 and `golangci-lint` v2.12.2 become build-time dependencies; this ADR is
their dependency record. Builds need the toolchain to fetch and compile them. In exchange the
architecture and conventions are enforced uniformly, and violations surface at `make verify`
time rather than in review or production.
