# yagocrawlcontract

`yagocrawlcontract` is the shared message contract between `yago-node` and the
optional crawler service. It exists as a separate Go module so both services can
exchange crawl work and crawl results without importing each other.

The Go types and JSON codec tests in this module are the source of truth for field
names, encoded shapes, and handle calculation. This README describes only the
behavioral contract that is not obvious from the type definitions.

## Message flow

The contract has a leased work flow and a feedback-bearing ingest flow:

```text
          WorkerRegistration + CrawlOrderDelivery(lease ID)
node --------------------------------------------------> crawler
     <---------------- AckOrder/NAK + Heartbeat + progress

          SubmitIngest(IngestBatch)
node <----------------------------------------------- crawler
     -----------------------------------------------> accepted/backpressure
```

`CrawlOrder` carries crawl work from the node to crawler instances. The order includes
the crawl profile and seed requests needed to start or continue a crawl.
Each seed request has a mode. Empty mode and `url` mean a normal page URL.
`sitemap` means an XML sitemap or sitemap index. `sitelist` means a plain text
URL list. Crawlers expand sitemap and sitelist starts into normal URL requests
before frontier admission.

`IngestBatch` carries references back to the node for one fetched page: document
content metadata, bounded image metadata, RWI postings, URL metadata, and the
attribution data needed by the node. Live pages and removal tombstones carry a
stable observation ID and UTC observation time so the node can order separate
deliveries and recognize a committed retry after an acknowledgement is lost.
Older batches without those fields remain accepted: observation time falls back
to `Document.FetchedAt`, and the node derives a stable identity from the batch.
The node persists the latest completed observation per source URL after ingest
side effects and before acknowledging the submission.

Multiple crawler processes register distinct worker identities, share the durable
order queue, and publish results to one node. Order settlement and ingest replies
remain bound to the crawler call that initiated them.

## Provenance

`Provenance` is an opaque node-owned token. The crawler never inspects or changes it;
it only echoes the token on ingest batches so the node can attribute results to the
order source.

Because attribution stays inside that token, local operator crawls and remotely
requested crawls use the same message shape.

## Request hints

`LastModified` on a crawl request is a scheduling hint carried from sitemap
`lastmod` values. It does not make the crawler trust page freshness by itself;
recrawl policy remains a crawler/frontier decision.

## Backpressure

Every streamed order has a durable lease ID. `AckOrder` deletes completed work;
the same call with requeue semantics naks unfinished work. `Heartbeat` renews the
leases held by one registered worker, and `ReportProgress` carries run tallies.
The node can durably enqueue more orders than a crawler currently has in its
frontier; crawler saturation is handled by lease ownership rather than by
blocking order creation.

Invalid order modes, URLs, profiles, deterministic fetch responses, and malformed
seed documents terminate the lease. Network, server, throttle, timeout, and
cancellation failures requeue it. Settlement calls are idempotent: a live crawler
retries transient ACK/NAK failures, while shutdown stops after a bounded detached
attempt so the lease can expire after heartbeats stop.

`SubmitIngest` is unary, so acceptance or retryable backpressure returns directly
to the crawler that submitted the batch. There is no shared feedback topic.

`IngestBatch` JSON is limited to 4 MiB minus 64 KiB of transport headroom, and
the enclosing gRPC message is limited to 4 MiB. The crawler bounds text, URLs,
headings, links, metadata, images, anchors, and postings before submission, then
fits optional collections to the encoded limit. Identity URLs over 2,048 bytes
are rejected, and overlong URL-bearing collection elements are dropped rather
than changed by truncation. The node reports temporary
pipeline or storage saturation as gRPC `Unavailable`; the crawler retries it
with a jittered exponential delay. It also retries the legacy `ResourceExhausted`
saturation code used by older nodes. Current crawler payloads are fitted below
the shared transport ceiling before either retry path can run.

## Crawl profile scope

The profile carries the subset of YaCy crawl settings that affect URL selection and
reference generation, including whether links marked `rel=nofollow` may be followed.
Crawler process settings, such as the browser User-Agent, are not profile fields because
an order cannot safely override process-wide browser identity.

Raw HTML bodies, binary image bodies, media indexing, snapshots, vocabulary
scraping, country and IP filters, HTTP caching, direct document loading, and
onward crawl redistribution are outside this contract.
