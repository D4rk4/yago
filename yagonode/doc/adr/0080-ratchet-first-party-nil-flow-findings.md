# 0080. Ratchet first-party nil-flow findings

Date: 2026-08-31

## Status

Accepted

Amends [ADR-0003](0003-automated-quality-gate.md).

## Context

A dereference reached by a nil value can stop a node or crawler process even
when ordinary tests and statement coverage exercise the surrounding code.
NilAway performs interprocedural Go nil-flow analysis, but its upstream project
is under active development and explicitly warns that false positives and
breaking analyzer changes can occur. The current node and crawler contain safe
nil-slice operations, heap slots, map invariants, and guarded return contracts
that the analyzer reports.

Turning off an entire package or file would also hide a new finding. Requiring
zero findings immediately would mix a broad historical cleanup into a release
gate change and invite low-quality defensive branches.

## Decision

Adopt `go.uber.org/nilaway` at exact version
`v0.0.0-20260808063849-8649a03c818a`. It is Apache-2.0 licensed and is a
build-time dependency only. Install its command from that exact version outside
the product module graphs and verify the installed binary's embedded module
identity.

Run NilAway separately in every production module with `include-pkgs` set to
that first-party module path. Exclude test files from production nil-flow
diagnostics. Disable pretty printing explicitly and require full file paths so
terminal capabilities, color conventions, and working-directory presentation
cannot change the ratchet input. Record the current 82 unique findings as
exact `file:line:column` locations in a sorted baseline. A release passes only
when the complete actual set equals the baseline. Any new, removed, moved, or
unclassified diagnostic requires an explicit baseline review; no first-party
production package, directory, source-file, or message-pattern exclusion is
permitted.

Keep the standalone process boundary because it is simple and currently fits
the release host. A custom golangci-lint plugin would provide modular fact
caching but would introduce a separately built linter distribution and another
configuration owner. Reconsider that driver only if measured release memory or
duration requires it.

Add policy tests that admit an exact finding set and refuse a new finding, a
removed finding, an analyzer failure, an unclassified diagnostic, an unsorted
baseline, a duplicate, and a non-location entry. Also refuse a runner that
allows ANSI pretty output or shortened file paths by removing either required
format flag.

## Consequences

New first-party production nil flows cannot enter silently, and resolved debt
cannot remain disguised as an accepted warning. The initial baseline is debt,
not proof that each location is safe; it must shrink through reviewed code
changes rather than speculative guards written only to satisfy an analyzer.

Line movement causes deliberate review noise. NilAway cannot prove nil safety,
does not analyze production behavior dynamically, and does not cover test-only
code in this gate. No runtime dependency, binary behavior, setting,
environment variable, service, stored format, contract, or wire shape changes.
