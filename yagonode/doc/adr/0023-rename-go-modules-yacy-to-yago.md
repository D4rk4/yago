# 23. Rename the Go modules and import paths from yacy* to yago*

Date: 2026-07-04

## Status

Accepted

Implements BRAND-01. Related: BRAND-02 (environment variables), BRAND-03
(containers, images, binaries), and BRAND-04 (the YagoSeek public brand).

## Context

The project's own Go naming still used a `yacy` prefix for the six workspace
modules (`yacycrawlcontract`, `yacycrawler`, `yacyegress`, `yacymodel`,
`yacynode`, `yacyproto`) and their import paths under
`github.com/D4rk4/yago/...`. The repository, the module root, and the node
binary had already moved to `yago`/`yago-node`, leaving the module layer
inconsistent with the rest of the project.

The constraint is that nothing a Java YaCy peer or a YaCy client observes on the
network may change. The `yacy` string therefore serves two very different roles:
it names *our* modules (safe to rename) and it names the *YaCy wire-compatibility
surface* (must be preserved).

## Decision

Rename the six workspace modules, their directories, `go.mod` module paths,
`go.work` entries, the module-root package names, every import and package
selector, and every build/config/doc reference from `yacy*` to `yago*`. `yacy`
and `yago` are both four characters, so the change is length-preserving — even
the go_package string embedded in generated `.pb.go` descriptors stays valid.

Deliberately **keep** the names that denote YaCy compatibility rather than our
own modules:

- Endpoint paths `/yacy/*` and `/yacysearch.{json,rss,html}`, and the
  `/Crawler_p.html` Unsupported marker.
- The protobuf wire package `yacycrawl.v1` (the gRPC contract's on-wire full
  names), even though its Go `go_package` import path is renamed.
- On-wire vocabulary and DTO field names in the protocol payloads.
- The legacy `yacy-rwi.db` data-file open fallback.
- The internal packages that implement the compatibility layer:
  `internal/yacysearch` (serves the YaCy search endpoints), the
  `test/fixtures/yacywire` golden fixtures, `yacy_hash_form.go` (the YaCy hash
  format), the `yacy-*.md` protocol/interop docs, and the `yacy_*.go` end-to-end
  interop tests.
- The `YaCy`/AGPL attribution required by the license.

The renamed node module therefore legitimately contains, for example,
`yagonode/internal/yacysearch` — "the yago node's YaCy-search compatibility
package" — which accurately reflects what that code is.

## Consequences

- Import paths and directories are consistent with the `yago` project name; a
  reader no longer has to reconcile a `yago` repository built from `yacy`
  modules.
- The rename is mechanical and reversible, applied as a single change with the
  full verify gate (fmt, vet, lint, arch, race tests, coverage, build) green and
  the Dockerized Semgrep and Trivy scans clean.
- The split between "our naming" and "YaCy-compatibility naming" is now explicit
  and documented, so future renames do not accidentally touch the wire surface.
- A pre-existing, intermittent data race in the crawler order-receiver test
  (background goroutines read the retry/heartbeat interval globals that tests
  mutate) surfaced under the race detector during this change and was fixed by
  capturing those intervals at construction and passing them as parameters.
