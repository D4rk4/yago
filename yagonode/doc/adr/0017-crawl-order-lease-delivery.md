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
  order, `AckOrder` with `requeue` clears the owner and holds a retryable failure
  behind a durable five-second retry deadline, and `AckOrder` with `terminate`
  removes an operator-cancelled or permanently invalid order. Legacy operations
  remain idempotent for a fixed 24-hour retry horizon. Rich terminal progress
  enters a fixed 24-hour confirmation horizon only after progress delivery and,
  for ACK, run-control completion are durable. Expiry atomically applies any
  still-pending requeue, while late valid token confirmation remains idempotent.
  The crawler retries transient settlement failures with bounded exponential delay
  while heartbeats remain live; shutdown gives settlement a detached five-second
  window and retains unresolved checkpoint-affined work. `Heartbeat` extends the
  deadline on every lease held by a worker.
- A stream reconnect for the same stable crawler identity atomically adopts,
  renews, and replays that worker's session-aware leases with their existing lease
  ids before it receives new FIFO work. Every process uses a fresh session
  identity, and stale sessions cannot mutate an adopted lease. A disconnect
  alone does not return leases to another worker. Each heartbeat RPC has a
  one-second client deadline. If a heartbeat omits or returns too late to renew an
  active lease, the crawler cancels its otherwise healthy order stream and
  reconnects, so the same worker immediately adopts the parked lease instead of
  waiting for an unrelated transport failure.
- A background sweeper reclaims deferred and legacy sessionless leases whose
  deadline has passed. Requeue uses a
  2.5-second sweep cadence under the default lease lifetime, avoiding both immediate
  live-stream redelivery loops and busy scanning. Node startup
  performs the same expiry check. Session-aware leases retain their stable-worker
  owner and lease id after the deadline, so the same worker can reconnect after a
  node restart and replay its leases before new FIFO work. Each crawler data
  directory stores one stable worker identity, and the
  checkpoint database's exclusive lock prevents concurrent processes from using
  it. A replacement using that directory can therefore replay its lease
  immediately. Graceful shutdown detaches queued work from memory without
  deleting its checkpoint, waits for in-flight work within its grace period, and
  stops heartbeating unfinished leases. A replacement using the same checkpoint
  adopts them immediately or after a longer outage; an unrelated worker cannot
  claim that checkpoint-affined traversal.

## Consequences

- Delivery becomes at-least-once: an in-flight page can be redelivered after a
  crash, while completed pages and the exact visited set are retained. Each
  admitted page keeps one observation identity and time across frontier replay;
  the node persists the newest completed observation per
  source URL after ingest side effects and before ACK. Older redeliveries and
  committed ACK-loss duplicates therefore converge without replacing newer
  state.
- A node restart does not make a live worker's unfinished order globally
  claimable. Its durable session-aware lease remains authoritative until that
  stable worker settles it, even after the process-session deadline.
- Duplicate terminal acknowledgement remains successful after a lost response.
  An acknowledgement for a lease already requeued by NAK or expiry is rejected,
  so it cannot delete crawler recovery state while another copy remains pending.
  Legacy deduplication expires after 24 hours. A tokenized rich settlement starts
  the same fixed horizon only after its delivery phase is durable; expiry performs
  any pending requeue atomically and later valid confirmation is still successful.
- The crawler retains the 4,096 most recent completed lease ids in process memory.
  A stale replay within that process is acknowledged again without starting a
  duplicate run, including when the first acknowledgement response was lost.
- The `CrawlExchange` contract carries a lease id on each order, stable worker
  and process-session identities, the exact active lease set in heartbeats, and
  the `AckOrder` and `Heartbeat` calls. This is the internal node-crawler control
  plane, not the YaCy P2P wire, so extending it does not affect YaCy
  compatibility. Node and crawler binaries must be upgraded as a matched pair.
- The lease deadline, heartbeat cadence, and heartbeat request timeout are
  internal defaults rather than operator configuration for now. Both heartbeat
  bounds stay comfortably below the lease deadline. An unexpected local lease
  loss is a stream-reconnect signal; an intentional settlement is not.
- Invalid orders, deterministic seed-content failures, and operator cancellation
  terminate without a poison retry loop. Network, server, throttle, timeout, and
  retryable fetch-abort failures nak the order for redelivery.
