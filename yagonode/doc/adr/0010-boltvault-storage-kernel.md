# 10. Own the embedded database behind a storage kernel

Date: 2026-06-22

## Status

Accepted

Supersedes the storage organization of [2. Separate HTTP handlers, domain logic, and adapters](0002-layered-architecture.md) for new code. Builds on [5. Use bbolt for embedded storage](0005-use-bbolt-for-embedded-storage.md).

## Context

With features organized as vertical modules, no module may reach another's stored bytes, and
the database schema must not be a cross-module contract. Something has to own the embedded
database file, hand each module access to only its own data, and enforce the storage
invariants — durability and length counts — in one place rather than by hand in every
feature.

Capacity is an admission decision: refuse new data when full, still permit eviction to delete.
Only the module admitting data can make it, so the kernel reports capacity and the modules
enforce it.

## Decision

`boltvault` is the single owner of the embedded database file. No module receives the raw
database handle. A module registers a collection over its own bucket once at startup, supplying
a codec for its `yagomodel` value type; the kernel ensures the bucket and a length counter and
rejects duplicate registration. A collection can touch only its registered bucket.

All access happens inside a transaction obtained from `Update` or `View`; there is no
auto-commit path and a transaction cannot escape its closure. Write methods called inside a
read-only transaction return an error. The kernel maintains each collection's length
automatically and commits a write transaction durably before `Update` returns.

`AtCapacity` reports whether used bytes have reached the quota. A module about to admit new
data consults it first and returns backpressure when full; eviction never asks, so it deletes
through the same `Update` path. An out-of-space failure from the operating system maps to the
same capacity signal.

A transaction is opaque and may be passed across module boundaries, so several modules can
mutate their own collections within one transaction — cross-module atomicity without a shared
schema. No bolt type appears on the kernel's exported surface; only `boltvault` imports bbolt,
and `.go-arch-lint.yml` enforces that.

## Consequences

There is exactly one path to mutate stored data, and the durability and count invariants live
in one place instead of in every feature. The kernel reports capacity and the data-admitting
modules decide on it, so it never carries a feature's intent. Modules work with their domain
values through a narrow generic interface and never see serialization or another module's
bytes. Because no bolt type leaks, the underlying engine can be replaced behind this interface.
The cost is a small generic layer between features and the database, which buys the ownership
and isolation above.
