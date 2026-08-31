# 0082. Reject reachable Go vulnerabilities

Date: 2026-08-31

## Status

Accepted

Amends [ADR-0003](0003-automated-quality-gate.md).

## Context

Filesystem and container scanners identify vulnerable installed versions but
cannot determine whether a Go program reaches the affected symbol. Conversely,
a module graph can contain an advisory for code that the products never call.
A source-aware release gate can add a lower-noise view without weakening Trivy
or the dependency review process.

## Decision

Adopt `golang.org/x/vuln/cmd/govulncheck` from exact module version v1.7.0.
The module is BSD-3-Clause licensed and is a build-time dependency only. Install
it outside every product module graph and verify its embedded module identity.

Run source-mode, symbol-level govulncheck against every package in all six
production modules. Test-only call graphs remain outside this production
reachability check. A reachable vulnerability, package-loading failure,
database failure, or analyzer failure blocks `make verify` and therefore every
release build. Use the canonical Go vulnerability database; do not turn an
unavailable or stale query into a successful release result.

Keep Dockerized Trivy source and image scans as independent feature-closure
gates. Govulncheck does not replace image operating-system advisories, secret
scanning, misconfiguration scanning, gosec, Semgrep, or manual dependency
review.

Add release-wiring tests that admit the complete direct prerequisite and refuse
its removal. The tool's own exit contract remains the finding boundary.

## Consequences

A release cannot knowingly ship a Go vulnerability that the source analyzer
can trace to a called symbol. Uncalled advisories remain visible in verbose
diagnostics but do not independently fail this gate; Trivy still evaluates
their installed versions.

Results depend on the current curated Go vulnerability database and static call
graph precision. Reflection, assembly, plugins, runtime data, private advisories,
and non-Go components may be invisible. The network-backed database also makes
verification fail closed during an outage. No runtime dependency, setting,
environment variable, listener, service, stored format, contract, or wire shape
changes.
