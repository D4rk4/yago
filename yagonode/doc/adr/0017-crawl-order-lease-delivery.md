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
  window and retains unresolved checkpoint-affined work. Periodic `Heartbeat`
  calls extend the complete active lease set with an explicit false delivery-
  confirmation marker, while a true marker targets exactly the newly received
  lease or recovery batch. An absent marker retains the legacy subset-
  confirmation rule for older crawlers.
- A stream reconnect for the same stable crawler identity atomically adopts,
  renews, and replays that worker's session-aware leases with their existing lease
  ids before it receives new FIFO work. Every process uses a fresh session
  identity, and stale sessions cannot mutate an adopted lease. A disconnect
  alone does not return leases to another worker. Each heartbeat RPC has a
  one-second client deadline. If a heartbeat omits or returns too late to renew an
  active lease, the crawler cancels its otherwise healthy order stream and
  reconnects, so the same worker immediately adopts the parked lease instead of
  waiting for an unrelated transport failure.
- Each ordinary order consumes one session-scoped delivery credit after its
  durable claim. The node registers the expected lease before sending it and does
  not claim or send the next order until a successful heartbeat renews that
  lease. A successful session-authorized disposition of the exact lease is also
  sufficient receipt evidence, so a malformed payload or an idempotent legacy
  settlement cannot strand the credit. The current crawler confirms an ordinary
  lease before decoding its payload. The confirmation wait holds neither the
  worker-session registry mutex nor a database transaction.
- A reconnect may adopt at most 1,024 active leases. The first recovery frame
  declares that complete session manifest once, and the node frames it as ordered
  batches of at most 16. It sends the first frame, waits for a targeted heartbeat
  that renews exactly every lease ID in the current batch, and only then sends the
  batch remainder. The crawler retains the manifest separately, accepts later
  batches only as nonrepeating subsets, requires exact completion before ordinary
  streaming, and never retains the complete set of order payloads. Periodic
  heartbeats keep the full manifest alive without confirming unseen batch credit.
- The broker rebuilds an in-memory worker/session lease-capacity catalog from the
  durable lease bucket when it opens. A capacity check is O(1); successful claim,
  adoption, settlement, defer, and requeue transitions update the catalog only
  after the corresponding durable mutation commits.
- Running progress verifies the exact lease, worker, session, and run once. That
  authorized run target is reused for control reconciliation and recording, so
  the report does not scan the complete lease bucket again. The first successful
  assignment is cached for the active run; repeat reports from the same worker
  avoid persistent directive reconciliation, worker migration reconciles before
  replacing the cache, and successful completion removes it. Human-facing run IDs
  derived from provenance bytes use lowercase hexadecimal text.
- Ingest validates the current process session and exact lease/run under the
  short lease-mutation boundary, then releases it before the document, search
  index, URL metadata, posting, stale-sweep, and recrawl writes. That successful
  snapshot is the authorization linearization point for idempotent absorption;
  storage does not retain a lease read fence while it completes.
- Each crawler process has a distinct active-task workbench, defaulting to 32 and
  live-resizable from 1 through 256. A task holds its slot from prepared-order
  admission through terminal completion, while waiting ordinary or recovered
  orders activate neither a frontier nor a progress reporter. Fetch-worker
  concurrency remains an independent control. A lower limit does not preempt
  existing tasks.
- The progress-delivery queue keeps its in-flight generation immutable, coalesces
  newer running state into one pending per-run replacement, serves other ready
  runs fairly before that replacement, and continues to prioritize protected
  terminal phase chains. The node run registry retains every active run; its
  capacity evicts only deterministic recent terminal history.
- The dedicated bbolt engine serializes writer admission with a context-aware
  token. A cancelled waiter does not enter bbolt, and an admitted update checks
  cancellation before its callback and before commit so stale RPC work rolls
  back.
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
  and process-session identities, bounded lease-ID sets in heartbeats, and the
  `AckOrder` and `Heartbeat` calls. Additive fields carry the complete recovered-
  session manifest, optional keepalive-versus-confirmation presence, and the live
  active-task directive. Older protobuf decoders ignore them, while the absent
  marker retains compatible legacy confirmation. This is the internal node-
  crawler control plane, not the YaCy P2P wire, so extending it does not affect
  YaCy compatibility. Node and crawler binaries must be upgraded as a matched
  pair.
- The lease deadline, heartbeat cadence, and heartbeat request timeout are
  internal defaults rather than operator configuration for now. Both heartbeat
  bounds stay comfortably below the lease deadline. An unexpected local lease
  loss is a stream-reconnect signal; an intentional settlement is not.
- Invalid orders, deterministic seed-content failures, and operator cancellation
  terminate without a poison retry loop. Network, server, throttle, timeout, and
  retryable fetch-abort failures nak the order for redelivery.
