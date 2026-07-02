# yacycrawlcontract

`yacycrawlcontract` is the shared message contract between `yago-node` and the
optional crawler service. It exists as a separate Go module so both services can
exchange crawl work and crawl results without importing each other.

The Go types and JSON codec tests in this module are the source of truth for field
names, encoded shapes, and handle calculation. This README describes only the
behavioral contract that is not obvious from the type definitions.

## Message flow

The contract has two one-way message flows:

```text
          CrawlOrder
node ------------------> crawler

          IngestBatch
node <------------------ crawler
```

`CrawlOrder` carries crawl work from the node to crawler instances. The order includes
the crawl profile and seed requests needed to start or continue a crawl.

`IngestBatch` carries references back to the node for one fetched page: document
content metadata, bounded image metadata, RWI postings, URL metadata, and the
attribution data needed by the node.

Each flow is one-way so multiple crawler instances can share the same work stream and
publish results back to one node without per-crawler addressing.

## Provenance

`Provenance` is an opaque node-owned token. The crawler never inspects or changes it;
it only echoes the token on ingest batches so the node can attribute results to the
order source.

Because attribution stays inside that token, local operator crawls and remotely
requested crawls use the same message shape.

## Backpressure

There is no per-order acknowledgement. The order stream is bounded: when crawlers are
saturated, publishing more work blocks or fails according to the queue implementation.
The node decides whether to accept more crawl work before it publishes an order.

There is also no per-batch feedback topic. Ingest backpressure belongs to the ingest
stream, and a shared reply topic could not reliably route feedback to the crawler that
published a specific batch.

## Crawl profile scope

The profile carries the subset of YaCy crawl settings that affect URL selection and
reference generation, including whether links marked `rel=nofollow` may be followed.
Crawler process settings, such as the browser User-Agent, are not profile fields because
an order cannot safely override process-wide browser identity.

Raw HTML bodies, binary image bodies, media indexing, snapshots, vocabulary
scraping, country and IP filters, HTTP caching, direct document loading, and
onward crawl redistribution are outside this contract.
