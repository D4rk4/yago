# 4. Lint and format with a pinned ruff binary

Date: 2026-07-07

## Status

Accepted

## Context

`make verify` pins every other lint/format tool as a checksum-verified binary in
`.toolchain/bin` (`golangci-lint`, `go-arch-lint`), fetched by `tools/install` from
`tools/tools.lock`, so the gate runs the recorded version regardless of what is on `PATH`. The
Python plugin had no equivalent: nothing checked its formatting or caught common mistakes.

## Decision

`ruff` is pinned in `tools/tools.lock` alongside the Go tools and fetched the same way: a
checksum-verified tarball per platform, installed to `.toolchain/bin/ruff`. `make fmt`/
`fmt-check` run `ruff format`, and `make lint` runs `ruff check`, against `plugins/searxng/searxng-result-router/`
as part of the same targets the Go modules use — not a separate plugin-only step.

## Consequences

`ruff` 0.15.20 becomes a pinned build-time dependency; this ADR is its record. The Python code
gets the same formatting and lint enforcement as the Go modules, through the same `make`
targets, with no reliance on a system-installed linter.
