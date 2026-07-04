# yacytextindexer — Technical Specification

## Context

`yacycrawler` discards document bodies by design, contributing only word-index postings
toward the DHT. Some operators want real full-text search over their own crawled corpus.
`yacytextindexer` is a separate, optional, disposable Go service that consumes the
crawled pages `yacycrawler` optionally publishes and indexes them into an operator-run
Elasticsearch instance. It serves no queries: SearXNG queries Elasticsearch directly
through its native Elasticsearch engine.

## Non-Goals

* Serving search queries or exposing any query API.
* Ranking, scoring, or judging relevance — that is Elasticsearch's job.
* Running or provisioning Elasticsearch itself.
* Crawling, fetching, or parsing pages — that is `yacycrawler`'s job.
* Assigning document identity or defining URL canonicalization — the crawler stamps and
  defines both; this service only carries the result.
* Storing text anywhere other than the operator's own Elasticsearch index.
* Participating in the YaCy DHT.

## Functional Requirements

* The service SHALL subscribe to the crawled-page subject `yacycrawler` publishes.
* The service SHALL translate each received crawled page into an Elasticsearch document
  carrying its canonical URL, title, extracted text, crawl timestamp, and language.
* The service SHALL use the crawler-supplied document identity as the Elasticsearch
  document identity, so re-indexing an already-seen URL overwrites the existing document
  rather than creating a duplicate.
* The service SHALL populate the field names SearXNG's Elasticsearch engine reads for a
  result's title, URL, and content, so a configured SearXNG returns a clickable title and
  a snippet for an indexed document.
* The service SHALL let operators configure the Elasticsearch endpoint and index name.
* On transient Elasticsearch unavailability, the service SHALL retry rather than drop a
  received page's content.
* While Elasticsearch remains unavailable, the service SHALL stop pulling new messages
  from its NATS subscription rather than accumulate an unbounded in-process backlog. Held
  messages remain in JetStream and are redelivered once the service resumes consuming.

## Non-Functional Requirements

* The service SHALL set an explicit limit on the number of documents it indexes
  concurrently.
* The service SHALL keep memory usage bounded independently of corpus size.
* The service SHALL persist no state of its own: the index of record lives in
  Elasticsearch and any pending backlog lives in JetStream. Its survival across an
  Elasticsearch outage therefore depends on operator-sized broker retention (see
  `yacycrawler`'s stream retention contract), not on any store this service owns.
* The service SHALL be independently disposable: operators MAY stop it and later re-enable
  the crawler's crawled-page sink without depending on this service's prior state.
* The service SHALL support running multiple instances concurrently against the same
  crawled-page subject, with each crawled page indexed by exactly one instance, so operators
  can scale indexing throughput horizontally by adding instances.

## Known Limitations

* Result quality and relevance tuning are entirely Elasticsearch's responsibility.
* Operators who enable this path take on Elasticsearch's own operational and
  data-retention weight, which the default crawler path does not carry.
* Idempotent overwrite keeps a recrawled URL fresh, but a URL that is never recrawled is
  never refreshed and a removed URL is never deleted: the index can hold stale documents.
  Freshness and deletion scheduling are out of scope.
* A sustained (non-transient) Elasticsearch outage stops this service consuming; the
  un-acked backlog grows inside JetStream's retention limits. If the outage outlasts the
  stream's configured retention, JetStream may drop the oldest held messages, which are
  then lost to full-text indexing and need a recrawl to recapture. This is a
  broker-retention limit, not one this service can absorb.
* Full-text search here (`yacytextindexer`→Elasticsearch→SearXNG) and DHT search
  (`yacydhtsearch`) are two independent SearXNG engines with divergent ranking and dedup
  and no unified result story. Accepted residual, not addressed here.
* A canonicalization contract version change alters document identity for every URL it
  affects. This service has no migration behavior: documents keyed under a superseded
  version are neither deleted nor merged with their newly-keyed recrawl, and the index
  accumulates both until an operator intervenes.
