# yacycrawler

> **Experimental prototype.** Not production-ready. Interfaces, message shapes, and
> behavior change without notice, and nothing here is stable to build on yet.

An optional, disposable crawl service that fetches URLs, builds YaCy-compatible RWI
postings and URL metadata, and publishes them toward a YaCy RWI node without storing
document bodies.

For what the package does and how the pieces fit together, see the package doc in
[`doc.go`](doc.go).

## Why two separate services

The RWI node is built to run unattended on Raspberry-Pi-class hardware: it stores and
serves the Reverse Word Index and deliberately does not crawl. Crawling is bursty,
CPU- and bandwidth-hungry, and benefits from a real browser engine — work that does not
belong on the always-on node.

So crawling lives here, as a **separate, optional, disposable** service meant to run on
a more powerful machine (a home PC you can freely turn off). It contributes exactly what
the YaCy DHT natively exchanges — *references*, not documents: word-index postings plus
URL metadata. No document bodies are stored or shipped anywhere.

```
                    ingest batches
                  (postings + metadata)
  ┌────────────┐  ───────────────────▶  ┌────────────────┐
  │ yacycrawler│        message          │   RWI node     │
  │ (this svc) │◀──────────────────────  │ (stores/serves │
  └────────────┘    crawl requests +     │      RWI)      │
   powerful host    backpressure          └────────────────┘
   on-demand        (future)               Pi-class, always-on
```

## Target architecture

The crawler is a pipeline of stages — frontier → fetch → parse → tokenize → build
postings + metadata → publish — wired together in
[`cmd/yacycrawler/main.go`](cmd/yacycrawler/main.go). Stages hand work to one another
only through a small queue seam (`Publisher` / `Receiver` over a bounded queue), never by
direct calls, so the topology can be reshaped or distributed without touching stage logic.

The same seam is the boundary between the crawler and the node. Today both ends live in
one process and the node side is faked, but the shape is the target shape:

- **Crawler → node (implemented, faked node):** each crawled page becomes an
  `IngestBatch` (`[]RWIEntry` postings + `[]URIMetadataRow` metadata + source URL) and is
  published to the ingest queue. A real node subscribes to that queue and merges the
  postings into its DHT buckets — the same contribution a Java YaCy peer makes via
  `transferRWI` / `transferURL`, minus the document body.
- **Node → crawler (future):** the queue is intentionally not request/response-bound, so
  the reverse direction is just more topics on the same seam — the node handing out crawl
  requests and signalling backpressure when its ingest is saturated.

This lets the prototype run **standalone**: no node, no network, no broker. The in-process
bounded queue stub stands in for a message broker, and `FakeNodeIngest` drains the ingest
queue and records what it received so the full fetch→publish path can be exercised in
tests.

### Why a message queue between them

- **Independent lifecycles.** The crawler can come and go (powered off, restarted, scaled
  out) while the node stays up; queued batches decouple the two.
- **Backpressure.** The queue is bounded, so a busy node naturally slows fast crawlers
  instead of being overwhelmed.
- **Fan-in / fan-out.** Multiple crawler instances can feed one node, and the node can
  later fan crawl requests back out, all over the same seam.
- **One swap point.** Replacing the in-process stub with a real broker is a single,
  well-isolated change that does not ripple into pipeline stages or the node.

## Known gaps

- The node ingest side is faked; there is no real broker or real node integration.
- URL hashing is not yet verified against the YaCy Java reference.
- Politeness and bot-wall handling are minimal heuristics, not hardened.
