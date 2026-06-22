# 7. Use NATS with JetStream for the node↔crawler message queue

Date: 2026-06-19

## Status

Draft

## Context

The node and the disposable crawl service communicate over a message queue rather than direct
calls. The seam has two one-way topics: crawl orders flow down to crawlers and ingest batches
flow up to the node. The prototype stands an in-process queue in for a broker; this decision
records the real broker.

The broker must satisfy the properties the seam already commits to:

- **Two one-way topics**, with no addressing that routes a result back to a specific sender.
- **Fan-in**: many crawler instances feed one node over a shared topic, and crawl orders fan
  back out, without per-instance addressing.
- **Backpressure, not acknowledgement**: when the consumer is saturated the publisher blocks;
  there are no per-message acks in the protocol.
- **Independent lifecycles**: crawlers are disposable and come and go while the node stays up;
  in-flight work should survive a crawler bounce, and the node re-sends orders on restart.
- **One swap point**: replacing the in-process stub is a single, isolated change that does not
  ripple into pipeline stages or the node.

The node is built to run unattended on Raspberry-Pi-class hardware, so the broker must be
light on memory and operationally simple, with no separate heavyweight runtime to manage.

## Decision

Use NATS as the broker, with JetStream enabled for durability and backpressure.

The two topics map to two subjects. Each is backed by a bounded, durable stream, so a full
stream slows the publisher instead of dropping work. Crawlers share a queue subscription on
the orders topic so work load-balances across instances; the node is the sole consumer of
ingest batches. Subjects carry no reply addressing, so many crawlers fan in to one node, and
durable streams let in-flight messages survive a crawler restart.

## Considered alternatives

Apache Kafka and Redpanda were considered for their durable, high-throughput logs. They were
rejected because their partition-centric model is overkill for two low-volume one-way topics,
and their resource footprint and operational surface are a poor fit for an always-on
Pi-class node.

RabbitMQ was considered because it offers mature work-queue semantics, fan-in, and
backpressure. It was rejected for the first broker because the Erlang runtime adds a heavier
process and operational story than a single static binary, without a matching benefit at this
scale.

Redis Streams was considered because it provides consumer groups and is widely deployed. It
was rejected because it bolts queue semantics onto a datastore: consumer groups, stream
trimming, and pending-entry timeouts would be managed by hand, whereas JetStream offers
bounded streams, flow control, and queue subscriptions as first-class primitives.

MQTT via Mosquitto was considered for its tiny footprint. It was rejected because its
strengths are lightweight fan-out telemetry, not durable work-queue fan-in with redelivery,
which is the core need here.

NSQ was considered because it shares the pure-Go, single-binary ethos. It was rejected
because it carries more moving parts and weaker momentum than NATS, with no offsetting
advantage for this seam.

## Consequences

NATS becomes a runtime dependency of the crawler and the node's order and ingest adapters;
this ADR is its dependency record. Operators run one NATS process with JetStream, a single
static binary that suits Pi-class hardware. Plain NATS alone is fire-and-forget and would drop
on a slow consumer, violating the backpressure requirement, so JetStream is required, not
optional. The broker sits behind the existing queue seam, so pipeline stages, the node, and
the standalone prototype path are unaffected.
