# yacycrawler — extracted-text capture sink

## What this is

`yacycrawler` has a second, opt-in output alongside its default postings output: an
extracted-text sink that carries parsed page text to a separate optional consumer,
`yacytextindexer`. The postings output stays the default and is unchanged by this sink's
existence.

## Functional Requirements

* The service SHALL publish postings-only `IngestBatch` messages on the existing ingest
  subject, unchanged, whether or not the extracted-text sink is enabled.
* The service SHALL discard raw HTML after parsing; raw HTML SHALL reach no sink.
* The service SHALL let operators enable the extracted-text sink independently of the
  postings sink.
* When the extracted-text sink is enabled, the service SHALL publish, per page, an
  artifact carrying: the canonical URL, the document identity derived from that canonical
  URL, the page title, the extracted text, the crawl timestamp, and the detected language.
* The service SHALL enforce a per-page text size limit before publishing. A page over the
  limit SHALL be dropped whole — not truncated and published — and the drop SHALL be
  logged, so a consumer never receives a partial page indistinguishable from a complete
  one.
* Enabling the extracted-text sink SHALL NOT alter the service's postings or metadata
  output.

### Canonical URL identity contract

This is the interface `yacytextindexer` depends on; it is stated here as the contract, not
as incidental implementation.

* Canonicalization SHALL normalize, in this fixed order: scheme (lowercased; `http` and
  `https` distinct — no scheme collapsing), host (lowercased), path (percent-decoded where
  safe, trailing slash removed except at root), query (parameters sorted by key, the
  operator-configured tracking-parameter list removed), and fragment (removed).
* The document identity SHALL be a stable hash of the URL canonicalized under the current
  contract version. URLs that canonicalize identically share one identity by design.
* This ruleset is version 1. Any change to what it normalizes SHALL be released as a new
  contract version and stated here, never silently altered.

## Trust boundary

* The extracted-text subject SHALL share the trust boundary of the NATS deployment the
  crawler is already configured against. Publish rights to it are equivalent to publish
  rights to ingest.
* Restricting or authorizing that subject beyond the configured deployment's boundary is
  operator-configured hardening, not core behavior, and SHALL NOT be baked into the
  crawler.

## Stream isolation and retention

* The postings and extracted-text sinks SHALL publish to separate JetStream streams with
  independently configured limits, so backlog or retention pressure on the extracted-text
  stream SHALL NOT affect publishing to the postings stream.
* The operator SHALL size the extracted-text stream's retention window. A consumer's
  outage-survival guarantee holds only within that window.

## Non-Goals

* Storing document bodies within `yacycrawler`, at rest, in any form.
* Guaranteeing delivery of captured text beyond standard NATS JetStream semantics.
* Ranking, indexing, or otherwise processing captured text — that is the consumer's job.

## Known Limitations

* Enabling the sink adds a data class (fetched web content) the default path does not
  carry, for as long as each page's text is being published.
* Unlike opaque postings, captured text renders directly in an operator's search UI. Any
  party with publish rights on the configured boundary can inject stored content into that
  UI. Widening the boundary without operator-added authorization accepts a
  content-injection risk that postings ingest never carried.
