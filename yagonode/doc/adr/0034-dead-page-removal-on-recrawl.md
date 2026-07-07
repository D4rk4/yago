# 0034. Remove a dead page from the index when a recrawl finds it permanently gone

Date: 2026-07-07

## Status

Accepted

## Context

RECRAWL-01 gave crawls a default recrawl cadence so indexed pages are re-fetched
on schedule instead of living forever. That closed the *staleness* half of the
"eternal index" gap; this ADR closes the other half: **a page that has been
removed from the web (HTTP 404/410) still lives in the index forever.**

Today, when the crawler re-fetches a URL and the origin answers 404 or 410, the
fetch is counted as a failed fetch (`page_fetcher.go` returns
`status N: ErrPageRejected`), nothing is emitted, and the previously-indexed
document, its postings, and its metadata are left untouched. The only mechanism
that ever deletes a URL is `internal/eviction`, and it deletes the *stalest*
URLs only under **storage-quota pressure** — never because a page is confirmed
dead. So a node that is not near its quota accumulates tombstoned-on-the-web
pages indefinitely, and searches keep returning results that 404 when clicked.

YaCy handles this on its own recrawl path (it records the HTTP failure against
the Solr document and can drop it). We have all the pieces — the crawler sees the
status, the node owns the index and already has a purge primitive
(`eviction.purgeURLs`, which drops a URL's postings and metadata in one
capacity-exempt transaction) — but nothing connects "the crawler saw a 404" to
"the node deletes the document."

Constraints that shape the design:

- The crawler and node are separate processes; the crawler is the only party that
  observes the HTTP status, and the node is the only party that owns the index.
- The crawler → node control plane is the `CrawlExchange` gRPC service. Its
  ingest RPC carries an **opaque `bytes batch_json`** whose shape is the
  contract's `IngestBatch` Go model — "the wire service is stable while the crawl
  data model stays authoritative in Go" (the proto's own comment). Ingest is
  already **at-least-once** with backpressure (ADR-0017).
- Deleting a live page by mistake is worse than keeping a dead one: a transient
  502, a timeout, a 403 behind a mis-configured WAF, or a network blip must never
  remove content.

## Decision

**1. A tombstone rides the existing ingest channel as an `IngestBatch` variant —
no proto change.** Add a boolean `Removed` field to the `yagocrawlcontract.IngestBatch`
model. A tombstone batch carries only `SourceURL`, `Provenance`, and
`ProfileHandle` with `Removed: true` and an empty document/postings/metadata. The
node's ingest path already unmarshals `IngestBatch` from `batch_json`
(`crawlbroker/exchange_server.go`), so the flag arrives with no change to
`crawlexchange.proto` and no code generation. This is backward compatible in a
mixed-version swarm: an old node ignores an unknown field (no purge, safe), and a
new node reading an old batch sees `Removed=false` (a normal ingest, safe).
Reusing the ingest channel inherits its durable at-least-once delivery and
backpressure for free, and a redelivered tombstone is harmless because the purge
is idempotent.

**2. The crawler emits a tombstone only on a permanent dead status: 404 or 410.**
`internal/pagefetch` gains a structured `GoneError{Status int}` (mirroring the
existing `ThrottledError`) that `page_fetcher` returns instead of the string-only
`status N: ErrPageRejected` for exactly 404 and 410. The pipeline, on a fetch
error, tests `errors.As(err, *GoneError)` and, when it matches, emits a tombstone
for the job's URL instead of a document. **Everything else keeps today's
behavior**: 403, 429, 5xx, timeouts, DNS/connection errors, and read errors are
transient or ambiguous and never delete content — they stay counted as failed
fetches. (429/503 already route to `ThrottledError`.)

A gone status must survive the crawler's fetch chain to reach the pipeline. The
fast HTTP client is the primary of a `FallbackPageSource` whose fallback is the
headless browser, and that source escalates a page rejection to the browser to
rescue bot-walled or JavaScript-rendered pages. A 404/410 is escalated too on the
unchanged path — but the browser receives the same status from the origin, so it
cannot rescue the page; it would instead render the server's error page into a
soft-404 document and bury the gone signal under a browser error, leaving the
recrawl path unable to tombstone the URL. So the fallback source treats a gone
status like a throttle: it propagates unchanged rather than escalating. This
matches how mainstream crawlers treat 404/410 as a removal signal rather than a
render candidate, and it stops the pre-existing habit of indexing rendered
error pages. (A site that returns 404 to bots but real content to a browser — rare
for the *404/410* codes specifically, as opposed to 403/challenge pages — is no
longer browser-rescued; that trade is accepted for correct, conservative
removal.)

**3. The node purges idempotently, so no "is this a recrawl?" flag is needed.**
On a `Removed` batch the node hashes `SourceURL` and drops that URL's postings
and metadata, reusing the eviction purge primitive (promoted to an exported
`eviction.Purge`/purger collaborator rather than duplicating the transaction).
Purging a URL that is not indexed is a no-op, so the crawler can emit a tombstone
for any 404/410 it sees — whether the URL was in the index (a recrawl of a
now-dead page, the target case) or not (a first crawl hitting a dead link, a
harmless no-op) — without the node needing to know which. The purge is one
capacity-exempt vault transaction, identical to eviction's, so postings and
metadata clear atomically.

## Alternatives considered

- **A dedicated `ReportGone` RPC on `CrawlExchange`.** Rejected: it churns the
  proto and adds a second delivery path with its own reliability story, when the
  ingest channel already provides exactly the durable, backpressured,
  at-least-once delivery a tombstone needs, and the contract model is explicitly
  the authoritative layer.
- **Marking the order as a recrawl and only purging then.** Rejected: the purge is
  already idempotent, so a recrawl flag adds state and a wire field for no benefit;
  emitting on any 404/410 also opportunistically cleans dead links found while
  following outlinks.
- **Deleting on any non-2xx.** Rejected as unsafe: a transient 5xx/timeout/403
  would evict live content. Only 404/410 (the web's "this resource does not/no
  longer exists" codes) are treated as permanent.
- **Soft-delete / tombstone-with-grace (require two consecutive dead recrawls
  before purge).** Deferred: it hedges against a site briefly mis-serving 404s,
  but adds per-URL state on the node; revisit if false deletions are observed in
  practice. The recrawl cadence already means a page must be dead at a scheduled
  recrawl, not on a single transient fetch.

## Consequences

- A page removed from the web is dropped from the index at its next recrawl, so
  search results stop returning permanently-dead links and the "eternal index"
  gap is fully closed (with RECRAWL-01).
- `IngestBatch` gains one optional field; the ingest codec, the crawler pipeline,
  `pagefetch`, and the node ingest absorb loop change, but `crawlexchange.proto`
  does not, and YaCy peer wire compatibility is untouched (this is the internal
  crawler↔node plane only).
- Deletion is deliberately conservative (404/410 only, idempotent, on the durable
  ingest channel). A site that mis-serves 404 for a live page during a scheduled
  recrawl would have that page dropped and re-added on the following healthy
  recrawl; if that proves to matter, the deferred two-strike grace is the next
  step.
