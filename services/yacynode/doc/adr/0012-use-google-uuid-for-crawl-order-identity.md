# 12. Use google/uuid to mint crawl order identity

Date: 2026-07-04

## Status

Accepted

## Context

A crawl order is delivered at least once: the broker redelivers it after a crawler crash, and
a stalled order can arrive again while its run is still in flight. The crawler keys a run by an
identifier so a redelivery re-runs the same work idempotently instead of starting a second
concurrent run. That identifier must be stable across a redelivery, so it is stamped once when
the node turns an operator request into an order, travels on the wire, and is parsed back by
the crawler.

The node needs to generate that identifier. It must be unique across orders without central
coordination and parseable back into a fixed-width value on the crawler side.

## Decision

Use `github.com/google/uuid` to mint the order identifier as a random (version 4) UUID,
stamped on the order at translation time and carried as a string on the wire.

The crawler already depends on this library to key runs, so the same UUID type spans both
sides of the contract. This ADR records it as a dependency of the node as well.

## Considered alternatives

A monotonic counter was rejected because it needs coordinated, durable state to stay unique
across node restarts, which the always-on Pi-class node should not carry for this.

Hashing the order content was rejected because two genuinely distinct orders with identical
seeds and profile would collide onto one run identity, and re-issuing the same crawl would be
indistinguishable from a redelivery.

Hand-rolling a random identifier over `crypto/rand` was rejected because it reinvents a format
the crawler already parses, for no benefit over the pinned library both sides share.

## Consequences

`google/uuid` becomes a runtime dependency of the node's crawl-dispatch adapter, matching the
version the crawler already pins. Every issued order carries a stable identity, so the crawler
can dedupe a redelivery against an in-flight run and re-run a post-crash redelivery cleanly
against idempotent node storage.
