# searxng-crawled-text-search — Technical Specification

## Context

`yacytextindexer` indexes crawled pages into an operator's own Elasticsearch instance, each
document following the shape defined by `searchdocument`. SearXNG's built-in `elasticsearch`
engine can query that index, but it always renders hits as a generic key/value table with no
`url` field — a shape `searxng-result-router` cannot rewrite, since it only rewrites the `url`
field of a standard result. `searxng-crawled-text-search` is a SearXNG engine module that queries
the same Elasticsearch index and returns each hit as a standard result carrying its title, URL,
and matched text, so it composes with `searxng-result-router` and closes the loop from crawl to
index to search to a tracked, re-crawlable click.

## Non-Goals

* Indexing pages into Elasticsearch — that is `yacytextindexer`'s job.
* Rewriting or routing result links — that is `searxng-result-router`'s job.
* Running or provisioning Elasticsearch itself.
* Tuning or reranking relevance beyond the query Elasticsearch is asked to run.
* Deduplicating, filtering, or otherwise curating results beyond what Elasticsearch returns.

## Functional Requirements

* For each Elasticsearch hit, the engine SHALL return a standard SearXNG result carrying the
  document's title, canonical URL, and matched text, reading the fields `searchdocument` defines.
* A result's URL SHALL be the document's canonical URL, unchanged, so a plugin such as
  `searxng-result-router` can still rewrite it.
* The engine SHALL match a document's title and content, favouring a title match, and return as
  matched text a content fragment around the search terms.
* The engine SHALL let operators configure the Elasticsearch endpoint and index name to query.
* The engine SHALL return an empty result set, not an error, when the search terms match no
  document.
* The engine SHALL fetch the page of results SearXNG asks for, consistent with SearXNG's own
  pagination.

## Non-Functional Requirements

* The engine SHALL persist no state of its own between requests.
* The engine SHALL add no observable delay beyond the Elasticsearch query itself.
* While Elasticsearch is unavailable, the engine SHALL return no results rather than fail the
  person's entire search.

## Known Limitations

* Relevance ordering is whatever Elasticsearch's full-text match returns beyond ranking a title
  match above a content match; the engine reranks nothing of its own.
* The engine depends on the document shape `searchdocument` defines; a change there is a change
  to this engine's contract, not something its configuration can absorb.
