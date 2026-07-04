# 14. Carry node↔crawler traffic over gRPC with a node-hosted queue

Date: 2026-07-03

## Status

Accepted

Supersedes [ADR-0007](0007-use-nats-jetstream-for-node-crawler-queue.md)

## Context

The node and the disposable crawl service exchange two one-way flows: crawl orders down to
crawlers, ingest batches back up. ADR-0007 stood a NATS JetStream broker in that seam. In
practice the broker was a third always-on process to run, secure, and reason about on
Pi-class hardware, and it duplicated durability the node already owns: the node has an
embedded transactional store (ADR-0010) that survives restarts. Running a separate durable
log beside a durable node is redundant operational surface.

The seam's committed properties still hold and must be preserved:

- **Two one-way flows** with no reply addressing.
- **Fan-in**: many crawlers feed one node; orders fan back out without per-instance addressing.
- **Backpressure, not acknowledgement**: a saturated node slows the crawler rather than
  dropping ingest.
- **Independent lifecycles**: crawlers are disposable; queued orders survive a node restart.
- **One swap point**: the node keeps the `CrawlOrderQueue` and ingest-stream ports the inner
  packages consume; only the edge adapter changes.

## Decision

Make the node the server and the crawler the client, speaking one gRPC service,
`CrawlExchange`, defined in the shared contract module and generated into `crawlrpc`. It has
two methods: `StreamOrders` (server-streaming) delivers claimed orders to a crawler as they
arrive; `SubmitIngest` (unary) hands one batch to the node and blocks until the node absorbs
it. Both payloads wrap the existing JSON codecs as opaque `bytes`, so the wire contract for
order and ingest bodies is unchanged.

Durability moves into the node. Orders are enqueued in a FIFO backed by the node's store, keyed
by a monotonic sequence, and deleted only once streamed to a worker; a queued order therefore
survives a node restart. Backpressure is the unary call itself: `SubmitIngest` blocks until the
ingest consumer takes the batch and returns `ResourceExhausted` when the pipeline is saturated,
on which the crawler retries. Internal control-plane traffic uses insecure transport credentials
on a private network.

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
from all three. Order delivery is now at-most-once from the crawler's view: the node forgets an
order once streamed, so an order in flight when a crawler dies is not redelivered, where
JetStream would have re-queued it. Ingest keeps its at-least-once guarantee through the blocking
call and crawler-side retry. The queue seam is unchanged, so pipeline stages, the ingest
consumer, and the crawl-dispatch endpoint are untouched.
