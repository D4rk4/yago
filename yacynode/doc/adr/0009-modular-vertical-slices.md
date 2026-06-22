# 9. Organize features as vertical slices over a storage kernel

Date: 2026-06-22

## Status

Accepted

Supersedes the layering of [2. Separate HTTP handlers, domain logic, and adapters](0002-layered-architecture.md) for new code.

## Context

The node was organized as horizontal layers (`api`, `core`, `infrastructure`) sharing one
storage object over four buckets. The database schema became an implicit cross-layer
contract: any layer could reach any bucket, counts were maintained by hand, and every
feature was smeared across three packages. Adding a feature meant editing all three layers,
and nothing prevented one feature from reading another's bytes.

## Decision

Features are bounded vertical modules under `internal/<module>/`. Each module owns its HTTP
endpoints, domain logic, and persistence together. Modules depend on each other only through
an explicit, acyclic graph of published Go interfaces over the `yacymodel` vocabulary; they
never share database buckets. A module asks another module for data through that module's
published port, never by reading its storage.

A small core carries shared vocabulary and mechanism with no feature logic: `yacymodel`,
`yacyproto`, the storage kernel `boltvault`, and the request-invariant guard `httpguard`.

The dependency graph is declared and enforced in `.go-arch-lint.yml`: each module is a
component listing exactly the modules and core packages it may depend on. The graph is a DAG.
Adding a new cross-module edge requires updating that file; the build fails on any edge that
is not declared there.

## Consequences

A feature lives in one package, with its endpoints, logic, and storage side by side. One
module cannot read another's bytes, so the schema is never a shared contract. The dependency
graph is explicit and mechanically enforced, so illegal edges fail the build instead of
accumulating. New modules land beside the existing layers and replace them slice by slice;
the layers remain until each slice supersedes them.
