# 2. Place crawl orders over NATS JetStream

Date: 2026-07-07

## Status

Accepted

## Context

A visit becomes one crawl order that a separate crawl fleet must pick up reliably, with
at-least-once delivery and backpressure the placement attempt can observe. The order consumer
(`yacycrawler`) and the `yacycrawlcontract` stream bindings already speak NATS JetStream on the
same orders stream and subject.

## Decision

We use `github.com/nats-io/nats.go` (pinned in `go.mod`) and its `jetstream` package to place
orders on the existing orders stream. The NATS/JetStream vocabulary is confined to
`crawlorderbroker`; the rest of the service speaks only the domain verb "place" through the
`CrawlOrderPlacement` port.

## Consequences

The broker edge is a single, replaceable package. A visit intake failure to place an order
(unreachable broker, full stream) surfaces as a plain error the bounded placement already
expects and records as unplaced, without blocking the redirect.
