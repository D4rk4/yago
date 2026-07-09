# yacytextindexer — Technical Specification

## Context

`yacytextindexer` is a separate, optional, disposable Go service that makes an operator's
own crawled corpus searchable as full text. `yacycrawler` can optionally publish the pages
it crawls; this service consumes those pages and indexes them into an operator-run
full-text search index.

## Non-Goals

* Serving search queries or exposing any query API.
* Running or provisioning the search index itself.
* Crawling, fetching, or parsing pages — that is `yacycrawler`'s job.
* Storing text anywhere other than the operator's own search index.

## Functional Requirements

* For each crawled page `yacycrawler` publishes, the service SHALL produce a search-index
  document preserving its canonical URL, text, and metadata.
* Re-indexing a page whose canonical URL is already indexed SHALL overwrite that document
  rather than add a duplicate.
* The service SHALL let operators choose which supported search index to use and configure
  its endpoint and index name.
* While the search index is unavailable, the service SHALL drop no page's content, resuming
  indexing once the search index returns.

## Non-Functional Requirements

* The service SHALL keep memory bounded independently of corpus size, capping how many
  documents it indexes concurrently.
* The service SHALL persist no state of its own: the index of record lives in the search
  index and any pending backlog lives with the broker.
* The service SHALL be independently disposable: operators MAY stop it and later re-enable
  the crawler's page-content output without depending on this service's prior state.
* The service SHALL support many concurrent instances over the crawler's published pages,
  with each page indexed by exactly one instance.

## Known Limitations

* A URL that is never recrawled is never refreshed and a removed URL is never deleted, so
  the index can hold stale documents; freshness and deletion scheduling are out of scope.
* Content held longer than the broker's retention while the search index is down is lost to
  indexing until a recrawl — a broker-retention limit this service can't absorb.
* If the crawler's canonicalization changes, a page's canonical URL changes with it; with
  no migration here, its old and new documents both persist until an operator intervenes.
