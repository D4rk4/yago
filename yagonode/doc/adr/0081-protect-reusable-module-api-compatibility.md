# 0081. Protect reusable-module API compatibility

Date: 2026-08-31

## Status

Accepted

Amends [ADR-0003](0003-automated-quality-gate.md).

## Context

The workspace publishes reusable model, protocol, crawl-contract, and egress
modules alongside the node and crawler applications. Tests inside the current
tree can remain green after an exported name, method, field, or signature is
removed, while an external consumer stops compiling.

The comparison must use the previous published release. An immutable tag whose
workflow failed before publication is release-attempt history and is not a
consumer baseline.

## Decision

Adopt `golang.org/x/exp/cmd/apidiff` from exact module version
`v0.0.0-20260824195058-e88cd73687aa`. The module is BSD-3-Clause licensed and
is a build-time dependency only. Install it outside every product module graph
and verify its embedded module identity.

Record both the previous published semantic-version tag and its full immutable
commit in `tools/api-compatibility-baseline`. Refuse a moved tag, a non-ancestor
baseline, or malformed identity. Export public API data from that exact commit
and the current tree for `yagomodel`, `yagoproto`, `yagocrawlcontract`, and
`yagoegress`; fail when apidiff reports an incompatible change. Internal
packages and the `yago-node` and `yago-crawler` application modules are outside
the reusable API promise.

Update the baseline only after a release is actually published. A deliberate
breaking reusable API requires its own compatibility decision and appropriate
versioning; changing the baseline is not an ordinary suppression mechanism.

Add policy tests that admit compatible output and refuse incompatible output,
a moved baseline tag identity, and removal of the gate prerequisite.

## Consequences

The release gate detects many source-level consumer breaks before publication
without adding a runtime dependency. Exact tag and commit pinning prevents a
failed tag or locally moved reference from silently changing the comparison.

Apidiff is an approximation. It cannot detect behavioral, wire, data-format,
performance, or semantic incompatibility, and some source changes require human
judgment even when compilation remains possible. Existing protocol golden and
contract tests remain authoritative for observable behavior.
