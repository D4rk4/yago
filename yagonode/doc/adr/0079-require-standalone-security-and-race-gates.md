# 0079. Require standalone security and race gates

Date: 2026-08-31

## Status

Accepted

Amends [ADR-0003](0003-automated-quality-gate.md).

## Context

The release workflow blocks on `make verify`. Race-instrumented tests already
ran there through a generically named `test` target and again during exact
coverage. Gosec rules ran only through the pinned golangci-lint aggregation.
Neither control was explicit in the release job, and the standalone security
scanner had no independently visible version or checksum identity.

The Go race detector instruments executed memory accesses and synchronization;
it reports only races reached by the test workload. Gosec statically checks Go
packages for security-relevant source patterns and exits unsuccessfully for an
unsuppressed finding or loading error. These are complementary release checks,
not substitutes for tests, review, Semgrep, Trivy, or runtime validation.

Yagomodel must calculate the legacy YaCy MD5 wire hash. That compatibility
primitive is not a password, signature, or trust decision. A repository-wide
weak-crypto exclusion would also admit unrelated future MD5 or SHA-1 use and is
therefore too broad.

## Decision

Install gosec v2.28.0 from its official Linux and Darwin amd64 and arm64 release
archives. Verify the recorded archive SHA-256 before installing the binary into
the repository toolchain directory. Gosec is Apache-2.0 licensed and is a
build-time dependency only.

Run the standalone scanner across all six production modules from an explicit
`gosec` prerequisite of `make verify`. Exclude generated Go files. Carry each
previously reviewed false-positive boundary as an exact file-and-rule pair in
`.gosec.json`; admit no directory-wide or repository-wide exception. This
includes G401 and G501 only for `yagomodel/yacy_hash_form.go`, so the same rules
remain active everywhere else. The configured Firefox launch exception remains
confined to the crawler path that revalidates its root-owned executable trust
boundary immediately before launch. Remove gosec from the golangci-lint
aggregation so one scanner invocation and one exclusion configuration own this
check.

Expose an explicit `race` target that runs `go test -race ./...` in every
production module. Keep `test` as an alias for operator compatibility. Make
`race`, rather than the alias, a direct prerequisite of `make verify`; retain
the race-instrumented exact-coverage run as an independent exercised-path gate.
Expose the blocking code-quality gate in the release workflow step and keep all
package and publication jobs dependent on its successful `verify` job.

Add repository tests that admit the complete wiring and refuse Makefile
variants missing either direct prerequisite. They also pin the standalone tool,
the exact file-and-rule exclusions, and the release-workflow invocation.

## Consequences

Every local or tagged `make verify` run uses the same checksum-pinned gosec
binary and blocks on any unsuppressed finding or package-loading error. A new
weak-crypto use outside the exact YaCy compatibility file is rejected. The
release log identifies static security analysis and race instrumentation
directly rather than relying on knowledge of an aggregate linter or alias.

Gosec static analysis can produce false positives and does not prove the
absence of a vulnerability. The race detector can observe only paths and
interleavings exercised by the tests. Findings must be corrected or narrowed
through an explicit policy change; source suppression comments remain
prohibited without operator approval.

No runtime dependency, binary content, stored format, setting, environment
variable, listener, service, package topology, contract, or wire shape changes.
