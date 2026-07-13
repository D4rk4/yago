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

The CRAWL-07 roadmap task asks for durable consumer-group behaviour and a worker
heartbeat so a disposable, restartable crawler fleet does not silently drop work.
The crawler already ran the acknowledgement seams (`Ack`/`Nak`/`Term`) as no-ops
because there was nothing on the node to settle.

## Decision

We lease each streamed order instead of deleting it, and let a worker keep its
leases alive with heartbeats.

- `StreamOrders` moves an order from the pending FIFO into a durable leased state
  tagged with a random lease id, a deadline, and the owning worker; the order
  bytes stay persisted until settlement.
- Two unary RPCs settle a lease: `AckOrder` with a lease id deletes a finished
  order, and `AckOrder` with `requeue` returns a cancelled order to the pending
  queue for another worker. Both operations are idempotent. The crawler retries
  transient settlement failures with bounded exponential delay while heartbeats
  remain live; shutdown gives settlement a detached five-second window and then
  lets any unresolved lease expire. `Heartbeat` extends the deadline on every
  lease held by a worker.
- A stream reconnect within one crawler process atomically renews and replays
  that worker's unexpired leases with their existing lease ids before it receives
  new FIFO work. A disconnect alone does not return leases to another worker.
- A background sweeper reclaims leases whose deadline has passed, and the node
  reclaims every outstanding lease at startup. Reclaimed orders keep their bytes
  but receive new lease identities when streamed again. Each crawler process
  appends a random suffix to its configured worker-name prefix, so an abrupt
  replacement waits for the old process's heartbeat expiry instead of claiming
  its leases. Graceful shutdown cancels queued local work, waits for in-flight
  work within its grace period, and naks every still-active lease for prompt
  redelivery.

## Consequences

- Delivery becomes at-least-once: a partially crawled order can be redelivered
  and crawled again. Each live page and tombstone carries a stable observation
  identity and time; the node persists the newest completed observation per
  source URL after ingest side effects and before ACK. Older redeliveries and
  committed ACK-loss duplicates therefore converge without replacing newer
  state.
- The crawler retains the 4,096 most recent completed lease ids in process memory.
  A stale replay within that process is acknowledged again without starting a
  duplicate run, including when the first acknowledgement response was lost.
- The `CrawlExchange` contract gains a lease id on each order plus the `AckOrder`
  and `Heartbeat` calls; this is the internal node-crawler control plane, not the
  YaCy P2P wire, so extending it does not affect YaCy compatibility.
- The lease deadline and heartbeat cadence are internal defaults rather than
  operator configuration for now; the heartbeat interval must stay comfortably
  below the lease deadline so a live worker never loses its in-flight orders.
- Invalid orders and deterministic seed-content failures terminate without a
  poison retry loop. Network, server, throttle, timeout, and cancellation failures
  nak the order for redelivery.
