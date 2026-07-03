# 17. Deliver crawl orders through durable leases with worker heartbeats

Date: 2026-07-03

## Status

Accepted

## Context

The node owns a durable queue of crawl orders and streams them to crawler
workers over the `CrawlExchange` gRPC service (ADR 0014). Until now the node
deleted an order the moment it was written to a worker stream. That is
at-most-once delivery: if a worker crashed, disconnected, or was killed mid-crawl
after receiving an order but before finishing it, that order's work was lost with
no redelivery, and the node had no signal that a worker was still alive.

PLAN.md CRAWL-07 asks for durable consumer-group behaviour and a worker
heartbeat so a disposable, restartable crawler fleet does not silently drop work.
The crawler already ran the acknowledgement seams (`Ack`/`Nak`/`Term`) as no-ops
because there was nothing on the node to settle.

## Decision

We lease each streamed order instead of deleting it, and let a worker keep its
leases alive with heartbeats.

- `StreamOrders` moves an order from the pending FIFO into a durable leased state
  tagged with a random lease id, a deadline, and the owning worker; the order
  bytes stay persisted so the lease survives a node restart.
- Two unary RPCs settle a lease: `AckOrder` with a lease id deletes a finished
  order, and `AckOrder` with `requeue` returns a cancelled order to the pending
  queue for another worker. `Heartbeat` extends the deadline on every lease held
  by a worker.
- A background sweeper reclaims leases whose deadline has passed, and the node
  reclaims every outstanding lease at startup, so a crashed or disconnected
  worker's orders are redelivered rather than stranded. The crawler heartbeats
  periodically and naks unfinished orders on graceful shutdown for prompt
  redelivery.

## Consequences

- Delivery becomes at-least-once: a partially crawled order can be redelivered
  and crawled again. Ingest already upserts documents, RWI postings, and URL
  metadata keyed by URL, so a duplicate crawl converges rather than corrupting
  state.
- The `CrawlExchange` contract gains a lease id on each order plus the `AckOrder`
  and `Heartbeat` calls; this is the internal node-crawler control plane, not the
  YaCy P2P wire, so extending it does not affect YaCy compatibility.
- The lease deadline and heartbeat cadence are internal defaults rather than
  operator configuration for now; the heartbeat interval must stay comfortably
  below the lease deadline so a live worker never loses its in-flight orders.
