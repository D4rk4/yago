# 14. Carry node↔crawler traffic over gRPC with a node-hosted queue

Date: 2026-07-03

## Status

Accepted; delivery semantics amended by [ADR-0017](0017-crawl-order-lease-delivery.md)

Supersedes [ADR-0007](0007-use-nats-jetstream-for-node-crawler-queue.md)

## Context

The node and the disposable crawl service exchange crawl orders, order settlement and
heartbeats, progress, and ingest batches. ADR-0007 stood a NATS JetStream broker in that seam. In
practice the broker was a third always-on process to run, secure, and reason about on
Pi-class hardware, and it duplicated durability the node already owns: the node has an
embedded transactional store (ADR-0010) that survives restarts. Running a separate durable
log beside a durable node is redundant operational surface.

The seam's committed properties still hold and must be preserved:

- **Fan-in**: many registered crawlers feed one node and share its durable order queue.
- **Explicit settlement**: leased orders remain durable until ACK, NAK, expiry, or startup reclaim.
- **Backpressure**: a saturated node slows the submitting crawler rather than dropping ingest.
- **Independent lifecycles**: crawlers are disposable; queued orders survive a node restart.
- **One swap point**: the node keeps the `CrawlOrderQueue` and ingest-stream ports the inner
  packages consume; only the edge adapter changes.

## Decision

Make the node the server and the crawler the client, speaking one gRPC service,
`CrawlExchange`, defined in the shared contract module and generated into `crawlrpc`.
`StreamOrders` registers a worker and server-streams leased orders; `AckOrder` settles or
requeues one lease; `Heartbeat` renews a worker's leases and carries control directives;
`ReportProgress` updates run tallies; and unary `SubmitIngest` hands one batch to the node
and waits until the node absorbs it. Order and ingest payloads wrap the existing JSON codecs
as opaque `bytes`.

Durability moves into the node. Orders are enqueued in a FIFO backed by the node's store,
keyed by a monotonic sequence, and move into durable worker leases when streamed. An ACK
deletes the order; a NAK, deadline expiry, or node startup returns it to the FIFO. Backpressure
is the unary ingest call itself: temporary pipeline or storage saturation returns
`Unavailable`, which the crawler retries with a jittered exponential delay. Ingest JSON is
bounded below the 4 MiB gRPC message ceiling. The crawler also retries `ResourceExhausted`
from older nodes that used it for application saturation; the current crawler fits the
payload before either retry path can run. Internal control-plane traffic uses insecure
transport credentials on a private network.

## Considered alternatives

Keeping NATS JetStream (ADR-0007) was the status quo. It was rejected because the durability
and backpressure it provides now duplicate the node's own store and the blocking unary call,
at the cost of a third always-on process on Pi-class hardware.

Plain HTTP between the two services was considered as the lightest possible transport. It was
rejected because server-streamed order delivery and typed, code-generated stubs on both sides
come for free with gRPC, whereas HTTP would hand-roll long-polling or chunked streaming and
its own status-code contract.

A node-hosted queue read over HTTP long-poll was considered to avoid a streaming protocol. It
was rejected because it reintroduces the polling latency and bespoke framing gRPC streaming
removes, with no offsetting simplicity once the queue already lives in the node.

## Consequences

The crawler no longer needs a broker address; it dials the node's crawl RPC endpoint
(`YAGOCRAWLER_NODE_RPC_ADDR`), and the node listens on `YAGO_CRAWL_RPC_ADDR`. gRPC and protobuf
become runtime dependencies of the node, the crawler, and the contract module; NATS is dropped
from all three. ADR-0017 makes order delivery at-least-once through durable leases and worker
heartbeats. Ingest keeps its at-least-once guarantee through the blocking call, durable
observation ordering, and crawler-side retry. The queue seam is unchanged, so pipeline stages,
the ingest consumer, and the crawl-dispatch endpoint are untouched.
