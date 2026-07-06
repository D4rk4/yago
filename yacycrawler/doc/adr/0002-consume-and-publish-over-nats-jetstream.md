# 2. Consume orders and publish outputs over NATS JetStream

Date: 2026-07-06

## Status

Accepted

## Context

The crawler idles until crawl orders arrive and must publish its outputs to a broker with
at-least-once delivery, acknowledgment, redelivery, and backpressure. The order producer
(`yacynode`) and the `yacycrawlcontract` stream bindings already speak NATS JetStream.

## Decision

We use `github.com/nats-io/nats.go` (pinned in `go.mod`) and its `jetstream` package for the
orders consumer and both output publishers. A full output stream (`MaxMsgs` + `DiscardNew`)
rejects a publish; that rejection is the transient backpressure the run waits out.

## Consequences

The broker edge is confined to `orderintake` and `pagepublication`. Correctness relies only on
at-least-once delivery with acknowledgment; the broker stays replaceable behind the crawl core's
narrow ports.
